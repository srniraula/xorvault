package main

import (
	"context"
	"dfs-project/dfspb"
	"dfs-project/pkg/config"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// MasterTracker keeps track of which master (primary or secondary) is currently
// active and handles automatic failover when the primary becomes unreachable.
type MasterTracker struct {
	mu            sync.RWMutex
	primaryAddr   string
	secondaryAddr string
	activeAddr    string
	failureCount  int
	maxFailures   int // consecutive failures before failover
}

// NewMasterTracker creates a tracker with the given primary and secondary addresses.
// It probes both masters at startup to discover which one is currently active,
// so a restarted chunkserver always connects to the correct master even after
// a failover occurred while this chunkserver was down.
func NewMasterTracker(primaryAddr, secondaryAddr string) *MasterTracker {
	active := resolveActiveMaster(primaryAddr, secondaryAddr)
	if active == "" {
		// Neither master is up yet at startup — default to primaryAddr and let
		// the heartbeat loop handle discovery once a master comes online.
		fmt.Fprintf(os.Stderr, "Startup probe: neither master active yet — defaulting to %s\n", primaryAddr)
		active = primaryAddr
	}
	return &MasterTracker{
		primaryAddr:   primaryAddr,
		secondaryAddr: secondaryAddr,
		activeAddr:    active,
		maxFailures:   3, // 3 x 2s heartbeat interval = 6 seconds before failover
	}
}

// resolveActiveMaster probes both master addresses via GetActiveMaster and returns
// the address of whichever one explicitly identifies itself as the active primary
// (isPrimary: true). Returns "" if neither node claims primary yet.
//
//  1. Ask primaryAddr   — if isPrimary:true, use it (normal case).
//  2. Ask secondaryAddr — if isPrimary:true, use it (post-failover case).
//  3. If neither claims primary → return "". Caller must keep waiting.
//
// IMPORTANT: Never fall back to a node that returned isPrimary:false.
// That caused an asymmetric failover bug: when active=secondary died, the
// primaryAddr fallback switched chunkservers to the standby primary immediately
// (before it promoted), but the reverse direction correctly waited. Returning ""
// forces symmetric waiting in both directions.
func resolveActiveMaster(primaryAddr, secondaryAddr string) string {
	probe := func(addr string) bool {
		if addr == "" {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return false
		}
		defer conn.Close()
		resp, err := dfspb.NewMasterServerClient(conn).GetActiveMaster(ctx, &dfspb.GetActiveMasterRequest{})
		return err == nil && resp.IsPrimary
	}

	if probe(primaryAddr) {
		fmt.Fprintf(os.Stderr, "Probe: %s is ACTIVE PRIMARY\n", primaryAddr)
		return primaryAddr
	}
	if probe(secondaryAddr) {
		fmt.Fprintf(os.Stderr, "Probe: %s is ACTIVE PRIMARY (promoted after failover)\n", secondaryAddr)
		return secondaryAddr
	}
	return ""
}

func (t *MasterTracker) ActiveAddr() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.activeAddr
}

func (t *MasterTracker) ReportSuccess() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failureCount = 0
}

// ReportFailure handles heartbeat failures and triggers failover if threshold is reached.
// Once the threshold is reached, it probes both masters on EVERY subsequent
// failure tick until one claims active, ensuring fast recovery.
func (t *MasterTracker) ReportFailure(logger *log.Logger) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.failureCount++
	logger.Printf("MasterTracker: heartbeat to %s failed (%d/%d consecutive failures)",
		t.activeAddr, t.failureCount, t.maxFailures)

	if t.failureCount < t.maxFailures || t.secondaryAddr == "" {
		return false
	}

	from := t.activeAddr
	newAddr := resolveActiveMaster(t.primaryAddr, t.secondaryAddr)

	if newAddr == "" {
		logger.Printf("Probe: no master claiming primary yet — waiting for promotion (retry next tick)")
		return false
	}

	if newAddr == from {
		logger.Printf("Probe: active master %s recovered — resetting failure count", from)
		t.failureCount = 0
		return false
	}

	logger.Printf("FAILOVER: active master %s unreachable — switching to %s", from, newAddr)
	fmt.Fprintf(os.Stderr, "\n╔══════════════════════════════════════════════════════════╗\n")
	fmt.Fprintf(os.Stderr, "║  ⚠️ MASTER UNREACHABLE — Searching for active master     ║\n")
	fmt.Fprintf(os.Stderr, "║  FROM: %-50s║\n", from+"  ")
	fmt.Fprintf(os.Stderr, "║  TO  : %-50s║\n", newAddr+"  ")
	fmt.Fprintf(os.Stderr, "╚══════════════════════════════════════════════════════════╝\n\n")
	t.activeAddr = newAddr
	t.failureCount = 0
	return true
}

type ChunkServer struct {
	dfspb.UnimplementedChunkServerServer
	storagePath                          string
	logger                               *log.Logger
}

func userDir(username string, clientID int64) string {
	if username != "" {
		return username
	}
	return fmt.Sprintf("%d", clientID)
}

// WriteChunk stores a chunk and its checksum to disk.
// Verifies data integrity by comparing received checksum with calculated checksum.
func (c *ChunkServer) WriteChunk(ctx context.Context, req *dfspb.WriteChunkRequest) (*dfspb.WriteChunkResponse, error) {
	if req.Checksum != "" {
		calculatedChecksum := calculateChecksum(req.Data)
		if calculatedChecksum != req.Checksum {
			c.logger.Printf("CHECKSUM MISMATCH for %s: received=%s, calculated=%s",
				req.ChunkId, req.Checksum, calculatedChecksum)
			return &dfspb.WriteChunkResponse{Success: false},
				fmt.Errorf("checksum verification failed: data corrupted in transit")
		}
		c.logger.Printf("Checksum verified for %s: %s", req.ChunkId, calculatedChecksum)
	}

	clientDir := filepath.Join(c.storagePath, userDir(req.Username, req.ClientId))
	err := os.MkdirAll(clientDir, 0755)
	if err != nil {
		c.logger.Printf("Failed to create client directory for %d: %v", req.ClientId, err)
		return &dfspb.WriteChunkResponse{Success: false}, err
	}

	path := filepath.Join(clientDir, req.ChunkId)
	err = os.WriteFile(path, req.Data, 0644)
	if err != nil {
		c.logger.Printf("WriteChunk failed for %s: %v", req.ChunkId, err)
		return &dfspb.WriteChunkResponse{Success: false}, err
	}

	if req.Checksum != "" {
		checksumPath := path + ".checksum"
		err = os.WriteFile(checksumPath, []byte(req.Checksum), 0644)
		if err != nil {
			c.logger.Printf("Failed to store checksum for %s: %v", req.ChunkId, err)
		}
	}

	c.logger.Printf("Stored %s for client %d (%d bytes)", req.ChunkId, req.ClientId, len(req.Data))
	return &dfspb.WriteChunkResponse{Success: true}, nil
}

// ReadChunk retrieves a chunk and its checksum from disk.
func (c *ChunkServer) ReadChunk(ctx context.Context, req *dfspb.ReadChunkRequest) (*dfspb.ReadChunkResponse, error) {
	clientDir := filepath.Join(c.storagePath, userDir(req.Username, req.ClientId))
	path := filepath.Join(clientDir, req.ChunkId)

	data, err := os.ReadFile(path)
	if err != nil {
		c.logger.Printf("ReadChunk failed for %s (client %d): %v", req.ChunkId, req.ClientId, err)
		return nil, err
	}

	checksum := ""
	checksumPath := path + ".checksum"
	if checksumData, err := os.ReadFile(checksumPath); err == nil {
		checksum = string(checksumData)
	} else {
		c.logger.Printf("Warning: checksum not found for %s", req.ChunkId)
	}

	c.logger.Printf("Sent %s for client %d (%d bytes, checksum: %s)", req.ChunkId, req.ClientId, len(data), checksum)
	return &dfspb.ReadChunkResponse{Data: data, Checksum: checksum}, nil
}

// DeleteChunks removes multiple chunks and their checksums from disk.
func (c *ChunkServer) DeleteChunks(ctx context.Context, req *dfspb.DeleteChunksRequest) (*dfspb.DeleteChunksResponse, error) {
	deletedCount := int32(0)

	for _, chunkID := range req.ChunkIds {
		var chunkPath string

		if req.ClientId == 0 && req.Username == "" {
			// Orphan cleanup: search all subdirectories for this chunk ID
			chunkPath = c.findOrphanChunkPath(chunkID)
			if chunkPath == "" {
				c.logger.Printf("Orphan chunk %s not found on disk (already gone)", chunkID)
				continue
			}
		} else {
			clientDir := filepath.Join(c.storagePath, userDir(req.Username, req.ClientId))
			chunkPath = filepath.Join(clientDir, chunkID)
		}

		checksumPath := chunkPath + ".checksum"

		err := os.Remove(chunkPath)
		if err != nil {
			if os.IsNotExist(err) {
				c.logger.Printf("Chunk %s not found (already deleted)", chunkID)
			} else {
				c.logger.Printf("Failed to delete chunk %s: %v", chunkID, err)
				continue
			}
		} else {
			c.logger.Printf("Deleted chunk: %s", chunkID)
			deletedCount++
		}

		err = os.Remove(checksumPath)
		if err != nil && !os.IsNotExist(err) {
			c.logger.Printf("Warning: Failed to delete checksum for %s: %v", chunkID, err)
		}
	}

	c.logger.Printf("Deleted %d/%d chunks for client %d", deletedCount, len(req.ChunkIds), req.ClientId)
	return &dfspb.DeleteChunksResponse{
		Success:      true,
		DeletedCount: deletedCount,
	}, nil
}

func (c *ChunkServer) findOrphanChunkPath(chunkID string) string {
	entries, err := os.ReadDir(c.storagePath)
	if err != nil {
		c.logger.Printf("findOrphanChunkPath: ReadDir failed: %v", err)
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(c.storagePath, entry.Name(), chunkID)
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate
		}
	}
	return ""
}

func sendSingleHeartbeat(masterAddr, myAddr string, logger *log.Logger) bool {
	conn, err := grpc.NewClient(masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Printf("Heartbeat: failed to connect to master %s: %v", masterAddr, err)
		return false
	}
	defer conn.Close()

	masterClient := dfspb.NewMasterServerClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = masterClient.ReceiveHeartbeat(ctx, &dfspb.HeartbeatRequest{Address: myAddr})
	if err != nil {
		logger.Printf("Heartbeat to %s failed: %v", masterAddr, err)
		return false
	}
	logger.Printf("Heartbeat sent to master %s from %s", masterAddr, myAddr)
	return true
}

// SendHeartbeats sends periodic heartbeats to the active master.
// addrOverride allows specifying a local IP manually when auto-detection fails.
func SendHeartbeats(port string, tracker *MasterTracker, logger *log.Logger, addrOverride string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	myAddr := addrOverride
	if myAddr == "" {
		myAddr = config.GetMyAddr(port)
	}
	logger.Printf("SendHeartbeats: will register with master as %s", myAddr)

	for range ticker.C {
		activeAddr := tracker.ActiveAddr()

		if ok := sendSingleHeartbeat(activeAddr, myAddr, logger); ok {
			tracker.ReportSuccess()
		} else {
			didFailover := tracker.ReportFailure(logger)
			if didFailover {
				newAddr := tracker.ActiveAddr()
				logger.Printf("Post-failover: sending immediate heartbeat to new master %s", newAddr)
				if sendSingleHeartbeat(newAddr, myAddr, logger) {
					tracker.ReportSuccess()
					logger.Printf("Post-failover heartbeat to %s succeeded", newAddr)
				}
			}
		}
	}
}
