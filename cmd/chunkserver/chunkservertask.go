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
func NewMasterTracker(primaryAddr, secondaryAddr string) *MasterTracker {
	return &MasterTracker{
		primaryAddr:   primaryAddr,
		secondaryAddr: secondaryAddr,
		activeAddr:    primaryAddr,
		maxFailures:   3, // 3 x 5s heartbeat interval = 15 seconds before failover
	}
}

// ActiveAddr returns the currently active master address.
func (t *MasterTracker) ActiveAddr() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.activeAddr
}

// ReportSuccess resets the failure counter (heartbeat succeeded).
func (t *MasterTracker) ReportSuccess() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failureCount = 0
}

// ReportFailure increments the failure counter and triggers failover if threshold
// is reached AND a secondary address is configured.
// Returns true if a failover just happened.
func (t *MasterTracker) ReportFailure(logger *log.Logger) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.failureCount++
	logger.Printf("MasterTracker: heartbeat to %s failed (%d/%d consecutive failures)",
		t.activeAddr, t.failureCount, t.maxFailures)

	if t.failureCount >= t.maxFailures && t.secondaryAddr != "" {
		from := t.activeAddr
		if t.activeAddr == t.primaryAddr {
			// Primary is down — fail over to secondary.
			logger.Printf("FAILOVER: primary master %s unreachable after %d failures — searching for secondary %s",
				t.primaryAddr, t.failureCount, t.secondaryAddr)
			fmt.Fprintf(os.Stderr, "\n╔══════════════════════════════════════════════════════════╗\n")
			fmt.Fprintf(os.Stderr,   "║  ⚠️ MASTER UNREACHABLE — Searching for active master     ║\n")
			fmt.Fprintf(os.Stderr,   "║  FROM: %-50s║\n", from+"  ")
			fmt.Fprintf(os.Stderr,   "║  TO  : %-50s║\n", t.secondaryAddr+"  ")
			fmt.Fprintf(os.Stderr,   "╚══════════════════════════════════════════════════════════╝\n\n")
			t.activeAddr = t.secondaryAddr
		} else {
			// Secondary is down — fail back to primary.
			logger.Printf("FAILBACK: secondary master %s unreachable after %d failures — searching for primary %s",
				t.secondaryAddr, t.failureCount, t.primaryAddr)
			fmt.Fprintf(os.Stderr, "\n╔══════════════════════════════════════════════════════════╗\n")
			fmt.Fprintf(os.Stderr,   "║  ⚠️ MASTER UNREACHABLE — Searching for active master     ║\n")
			fmt.Fprintf(os.Stderr,   "║  FROM: %-50s║\n", from+"  ")
			fmt.Fprintf(os.Stderr,   "║  TO  : %-50s║\n", t.primaryAddr+"  ")
			fmt.Fprintf(os.Stderr,   "╚══════════════════════════════════════════════════════════╝\n\n")
			t.activeAddr = t.primaryAddr
		}
		t.failureCount = 0
		return true
	}
	return false
}

// ChunkServer stores chunk data on disk and handles read/write requests
type ChunkServer struct {
	dfspb.UnimplementedChunkServerServer // don't know what it does.
	storagePath                          string
	logger                               *log.Logger
}

// userDir returns the subdirectory name: username when non-empty, numeric clientID otherwise.
// This controls how chunks are organized on disk under the chunkserver storage path.
func userDir(username string, clientID int64) string {
	if username != "" {
		return username
	}
	return fmt.Sprintf("%d", clientID)
}

// WriteChunk stores a chunk and its checksum to disk
// Uses client-specific subdirectories for physical isolation
// Verifies data integrity by comparing received checksum with calculated checksum
func (c *ChunkServer) WriteChunk(ctx context.Context, req *dfspb.WriteChunkRequest) (*dfspb.WriteChunkResponse, error) {
	// Data Integrity Verification: Calculate checksum on received data
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

	// Create client subdirectory: storagePath/username_or_id/
	clientDir := filepath.Join(c.storagePath, userDir(req.Username, req.ClientId))
	err := os.MkdirAll(clientDir, 0755)
	if err != nil {
		c.logger.Printf("Failed to create client directory for %d: %v", req.ClientId, err)
		return &dfspb.WriteChunkResponse{Success: false}, err
	}

	// Store chunk in client directory
	path := filepath.Join(clientDir, req.ChunkId)

	// Write chunk data
	err = os.WriteFile(path, req.Data, 0644)
	if err != nil {
		c.logger.Printf("WriteChunk failed for %s: %v", req.ChunkId, err)
		return &dfspb.WriteChunkResponse{Success: false}, err
	}

	// Store checksum
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

// ReadChunk retrieves a chunk and its checksum from disk
// Reads from client-specific subdirectory
func (c *ChunkServer) ReadChunk(ctx context.Context, req *dfspb.ReadChunkRequest) (*dfspb.ReadChunkResponse, error) {
	// Read from client subdirectory: storagePath/username_or_id/chunk_id
	clientDir := filepath.Join(c.storagePath, userDir(req.Username, req.ClientId))
	path := filepath.Join(clientDir, req.ChunkId)

	// Read chunk data
	data, err := os.ReadFile(path)
	if err != nil {
		c.logger.Printf("ReadChunk failed for %s (client %d): %v", req.ChunkId, req.ClientId, err)
		return nil, err
	}

	// Read checksum
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

// DeleteChunks removes multiple chunks and their checksums from disk
// Batched deletion for efficiency.
// If ClientId==0 and Username=="", search all user subdirectories for the chunk (orphan cleanup).
func (c *ChunkServer) DeleteChunks(ctx context.Context, req *dfspb.DeleteChunksRequest) (*dfspb.DeleteChunksResponse, error) {
	deletedCount := int32(0)

	// Delete each chunk in the batch
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
			// Construct path: storagePath/username_or_id/chunk_id
			clientDir := filepath.Join(c.storagePath, userDir(req.Username, req.ClientId))
			chunkPath = filepath.Join(clientDir, chunkID)
		}

		checksumPath := chunkPath + ".checksum"

		// Delete chunk file
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

		// Delete checksum file (ignore if doesn't exist)
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

// findOrphanChunkPath searches all user subdirectories of storagePath for a chunk file.
// Used when the master sends an orphan-cleanup DeleteChunks with clientId=0, username="".
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

// sendSingleHeartbeat sends one heartbeat to the given master address.
// Returns true on success, false on failure.
func sendSingleHeartbeat(masterAddr, myAddr string, logger *log.Logger) bool {
	conn, err := grpc.NewClient(masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Printf("Heartbeat: failed to connect to master %s: %v", masterAddr, err)
		return false
	}
	defer conn.Close()

	masterClient := dfspb.NewMasterServerClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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
// If the primary master becomes unreachable (3 consecutive failures), it
// automatically fails over to the secondary master address (if provided).
func SendHeartbeats(port string, tracker *MasterTracker, logger *log.Logger) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	myAddr := config.GetMyAddr(port)

	var conn *grpc.ClientConn

	for range ticker.C {
		activeAddr := tracker.ActiveAddr()

		if ok := sendSingleHeartbeat(activeAddr, myAddr, logger); ok {
			tracker.ReportSuccess()
		} else {
			didFailover := tracker.ReportFailure(logger)
			if didFailover {
				// Immediately try the new (secondary) master so it registers us quickly.
				newAddr := tracker.ActiveAddr()
				logger.Printf("Post-failover: sending immediate heartbeat to new master %s", newAddr)
				if sendSingleHeartbeat(newAddr, myAddr, logger) {
					tracker.ReportSuccess()
					logger.Printf("Post-failover heartbeat to %s succeeded — chunk server re-registered", newAddr)
				}
			}
		}
	}
	if conn != nil {
		conn.Close()
	}
}
