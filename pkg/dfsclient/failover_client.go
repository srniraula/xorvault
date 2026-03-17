package dfsclient

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"dfs-project/dfspb"
	"dfs-project/pkg/config"
)

// FailoverClient wraps multiple master addresses and transparently reconnects
// to the next available master when the current one becomes unreachable.
// It satisfies the Client interface so it can be dropped in anywhere a
// *GrpcClient is used today.
type FailoverClient struct {
	mu      sync.Mutex
	addrs   []string // all known master addresses
	current int      // index into addrs of the currently active master
	inner   *GrpcClient
}

// NewFailoverClient creates a FailoverClient that tries each address in addrs
// until one succeeds.  If addrs is empty it falls back to config.GetMasterAddrs().
func NewFailoverClient(addrs []string) (*FailoverClient, error) {
	if len(addrs) == 0 {
		addrs = config.GetMasterAddrs()
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no master addresses configured")
	}

	fc := &FailoverClient{addrs: addrs}
	if err := fc.connectToAny(); err != nil {
		return nil, err
	}
	return fc, nil
}

// connectToAny iterates over all addresses and connects to the first one that
// is the active primary master (IsPrimary=true). If none is active, it falls
// back to whichever master is reachable, so reads still work on a standby.
func (fc *FailoverClient) connectToAny() error {
	// First pass: find the active primary
	for i, addr := range fc.addrs {
		cli, err := NewGrpcClient(addr)
		if err != nil {
			continue
		}
		if fc.isActivePrimary(cli) {
			fc.inner = cli
			fc.current = i
			log.Printf("[FailoverClient] connected to active primary master at %s", addr)
			return nil
		}
		_ = cli.Close()
	}

	// Second pass: fall back to any reachable master (even standby)
	for i, addr := range fc.addrs {
		cli, err := NewGrpcClient(addr)
		if err != nil {
			continue
		}
		probeCtx, probeCancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, probeErr := cli.ListFiles(probeCtx, 0)
		probeCancel()

		if probeErr == nil || !isRetryable(probeErr) {
			fc.inner = cli
			fc.current = i
			log.Printf("[FailoverClient] connected to master at %s (may be standby)", addr)
			return nil
		}
		_ = cli.Close()
	}

	// No master responded; connect to the first address optimistically.
	cli, err := NewGrpcClient(fc.addrs[0])
	if err != nil {
		return fmt.Errorf("could not connect to any master: last error: %w", err)
	}
	fc.inner = cli
	fc.current = 0
	log.Printf("[FailoverClient] no master responded to probe; connected optimistically to %s", fc.addrs[0])
	return nil
}

// reconnect closes the current (failed) connection and tries the remaining
// addresses in round-robin order, preferring the active primary.
// Must be called with fc.mu held.
// It ALWAYS leaves fc.inner non-nil so callers never get a nil GrpcClient.
func (fc *FailoverClient) reconnect() error {
	if fc.inner != nil {
		_ = fc.inner.Close()
		fc.inner = nil
	}

	n := len(fc.addrs)
	// First pass: find the active primary
	for i := 1; i <= n; i++ {
		idx := (fc.current + i) % n
		addr := fc.addrs[idx]
		cli, err := NewGrpcClient(addr)
		if err != nil {
			log.Printf("[FailoverClient] could not connect to %s: %v", addr, err)
			continue
		}
		if fc.isActivePrimary(cli) {
			fc.inner = cli
			fc.current = idx
			log.Printf("[FailoverClient] failed over to active primary at %s", addr)
			return nil
		}
		_ = cli.Close()
		log.Printf("[FailoverClient] master at %s not active primary", addr)
	}

	// Second pass: fall back to any reachable master
	for i := 1; i <= n; i++ {
		idx := (fc.current + i) % n
		addr := fc.addrs[idx]
		cli, err := NewGrpcClient(addr)
		if err != nil {
			continue
		}
		probeCtx, probeCancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, probeErr := cli.ListFiles(probeCtx, 0)
		probeCancel()

		if probeErr == nil || !isRetryable(probeErr) {
			fc.inner = cli
			fc.current = idx
			log.Printf("[FailoverClient] failed over to master at %s (may be standby)", addr)
			return nil
		}
		_ = cli.Close()
	}

	// All probes failed. Keep a live (but unresponsive) connection so fc.inner
	// is never nil — callers will get a proper gRPC error instead of a panic.
	if cli, err := NewGrpcClient(fc.addrs[fc.current]); err == nil {
		fc.inner = cli
		log.Printf("[FailoverClient] all masters unreachable; holding optimistic connection to %s", fc.addrs[fc.current])
	}
	return fmt.Errorf("all master addresses unreachable")
}

// isRetryable returns true for gRPC transport/connectivity errors or STANDBY
// mode responses that suggest the master is down/inactive and we should try
// the next one.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, kw := range []string{
		"Unavailable", "unavailable",
		"connection refused", "EOF",
		"transport is closing", "no route to host",
		"connection reset", "broken pipe",
		"STANDBY", "standby",
	} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

// withRetry executes fn using the current inner client. On a retryable error
// it attempts to reconnect once and retries fn on the new master.
func (fc *FailoverClient) withRetry(fn func(*GrpcClient) error) error {
	fc.mu.Lock()
	inner := fc.inner
	fc.mu.Unlock()

	// Guard: if inner is nil (should not happen, but be safe) try to reconnect
	// before making any RPC to avoid a nil-pointer panic.
	if inner == nil {
		log.Printf("[FailoverClient] inner client is nil; attempting reconnect before RPC")
		fc.mu.Lock()
		_ = fc.reconnect()
		inner = fc.inner
		fc.mu.Unlock()
		if inner == nil {
			return fmt.Errorf("no master connection available")
		}
	}

	err := fn(inner)
	if !isRetryable(err) {
		return err
	}

	log.Printf("[FailoverClient] retryable error detected (%v); attempting failover…", err)
	fc.mu.Lock()
	rerr := fc.reconnect()
	inner = fc.inner
	fc.mu.Unlock()

	if rerr != nil {
		return fmt.Errorf("failover failed: %w (original: %v)", rerr, err)
	}
	if inner == nil {
		return fmt.Errorf("no master connection available after failover")
	}
	return fn(inner)
}

// --- Client interface implementation ---

func (fc *FailoverClient) ListFiles(ctx context.Context, clientID int64) ([]string, error) {
	var result []string
	err := fc.withRetry(func(c *GrpcClient) error {
		var e error
		result, e = c.ListFiles(ctx, clientID)
		return e
	})
	return result, err
}

func (fc *FailoverClient) DeleteFile(ctx context.Context, clientID int64, filename string, username string) (int, error) {
	var result int
	err := fc.withRetry(func(c *GrpcClient) error {
		var e error
		result, e = c.DeleteFile(ctx, clientID, filename, username)
		return e
	})
	return result, err
}

func (fc *FailoverClient) UploadFile(ctx context.Context, clientID int64, filename string, data io.ReadSeeker, size int64, username string) (int64, error) {
	var result int64
	err := fc.withRetry(func(c *GrpcClient) error {
		// Rewind the reader to the beginning for each attempt (important on retry)
		if _, seekErr := data.Seek(0, io.SeekStart); seekErr != nil {
			return fmt.Errorf("failed to rewind upload data: %w", seekErr)
		}
		var e error
		result, e = c.UploadFile(ctx, clientID, filename, data, size, username)
		return e
	})
	return result, err
}

func (fc *FailoverClient) DownloadFile(ctx context.Context, clientID int64, filename string, destPath string, username string) error {
	return fc.withRetry(func(c *GrpcClient) error {
		return c.DownloadFile(ctx, clientID, filename, destPath, username)
	})
}

// Close releases the current connection.
func (fc *FailoverClient) Close() error {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if fc.inner != nil {
		return fc.inner.Close()
	}
	return nil
}

// ActiveAddr returns the address of the master the client is currently connected to.
func (fc *FailoverClient) ActiveAddr() string {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if fc.inner != nil {
		return fc.inner.masterAddr
	}
	return ""
}

// isActivePrimary probes a master via GetActiveMaster and returns true only if
// it responds and reports IsPrimary=true.
func (fc *FailoverClient) isActivePrimary(cli *GrpcClient) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := cli.masterCli.GetActiveMaster(ctx, &dfspb.GetActiveMasterRequest{})
	if err != nil {
		return false
	}
	return resp.IsPrimary
}
