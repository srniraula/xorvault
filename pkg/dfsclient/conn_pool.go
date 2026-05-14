package dfsclient

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// ChunkConnPool maintains one persistent gRPC connection per chunkserver address.
// Instead of creating and closing a connection for every chunk upload/download,
// callers use Get() to obtain a long-lived *grpc.ClientConn that is reused across
// all RPCs to that server. HTTP/2 multiplexing within each connection handles
// any concurrent RPCs safely.
type ChunkConnPool struct {
	mu    sync.Mutex
	conns map[string]*grpc.ClientConn
}

func NewChunkConnPool() *ChunkConnPool {
	return &ChunkConnPool{
		conns: make(map[string]*grpc.ClientConn),
	}
}

// defaultPool is the package-level singleton used by writeChunkToServer and
// downloadChunkFromServer. It lives for the lifetime of the process.
var defaultPool = NewChunkConnPool()

// Get returns an existing connection for addr, or creates a new one with
// keepalive parameters. The returned connection must NOT be closed by the caller.
func (p *ChunkConnPool) Get(addr string) (*grpc.ClientConn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if conn, ok := p.conns[addr]; ok {
		return conn, nil
	}

	// Create a new persistent connection with keepalive to detect dead peers.
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second, // send ping every 10s if idle
			Timeout:             3 * time.Second,  // wait 3s for ping ack before considering dead
			PermitWithoutStream: true,             // ping even when no active RPCs
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc pool: failed to create connection to %s: %w", addr, err)
	}

	p.conns[addr] = conn
	log.Printf("[ConnPool] new persistent connection to %s", addr)
	return conn, nil
}

// Evict closes and removes the connection for addr. The next Get() call for
// this addr will create a fresh connection. This should be called when an RPC
// fails with a transient/transport error, so the retry uses a clean connection.
func (p *ChunkConnPool) Evict(addr string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if conn, ok := p.conns[addr]; ok {
		_ = conn.Close()
		delete(p.conns, addr)
		log.Printf("[ConnPool] evicted connection to %s", addr)
	}
}

// CloseAll closes every connection in the pool. Call on process shutdown.
func (p *ChunkConnPool) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for addr, conn := range p.conns {
		_ = conn.Close()
		log.Printf("[ConnPool] closed connection to %s", addr)
	}
	p.conns = make(map[string]*grpc.ClientConn)
}

// maxChunkRetries is the number of extra attempts after the first failure.
const maxChunkRetries = 2

// isTransientErr returns true for gRPC transport/connectivity errors that
// suggest the connection is broken and a retry on a fresh connection may succeed.
func isTransientErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, kw := range []string{
		"Unavailable", "unavailable",
		"connection refused", "EOF",
		"transport is closing", "no route to host",
		"connection reset", "broken pipe",
		"stream terminated", "server closed",
	} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}
