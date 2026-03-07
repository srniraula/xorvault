package main

import (
	"context"
	"dfs-project/dfspb"
	"dfs-project/pkg/config"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ChunkServer stores chunk data on disk and handles read/write requests
type ChunkServer struct {
	dfspb.UnimplementedChunkServerServer // don't know what it does.
	storagePath                          string
	logger                               *log.Logger
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

	// Create client subdirectory: storagePath/username/
	clientDir := filepath.Join(c.storagePath, req.Username)
	err := os.MkdirAll(clientDir, 0755)
	if err != nil {
		c.logger.Printf("Failed to create client directory for %s: %v", req.Username, err)
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

	c.logger.Printf("Stored %s for user %s (%d bytes)", req.ChunkId, req.Username, len(req.Data))
	return &dfspb.WriteChunkResponse{Success: true}, nil
}

// ReadChunk retrieves a chunk and its checksum from disk
// Reads from client-specific subdirectory
func (c *ChunkServer) ReadChunk(ctx context.Context, req *dfspb.ReadChunkRequest) (*dfspb.ReadChunkResponse, error) {
	// Read from client subdirectory: storagePath/username/chunk_id
	clientDir := filepath.Join(c.storagePath, req.Username)
	path := filepath.Join(clientDir, req.ChunkId)

	// Read chunk data
	data, err := os.ReadFile(path)
	if err != nil {
		c.logger.Printf("ReadChunk failed for %s (user %s): %v", req.ChunkId, req.Username, err)
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

	c.logger.Printf("Sent %s for user %s (%d bytes, checksum: %s)", req.ChunkId, req.Username, len(data), checksum)
	return &dfspb.ReadChunkResponse{Data: data, Checksum: checksum}, nil
}

// DeleteChunks removes multiple chunks and their checksums from disk
// Batched deletion for efficiency
func (c *ChunkServer) DeleteChunks(ctx context.Context, req *dfspb.DeleteChunksRequest) (*dfspb.DeleteChunksResponse, error) {
	deletedCount := int32(0)

	// Delete each chunk in the batch
	for _, chunkID := range req.ChunkIds {
		// Construct path: storagePath/username/chunk_id
		clientDir := filepath.Join(c.storagePath, req.Username)
		chunkPath := filepath.Join(clientDir, chunkID)
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

	// Try to clean up the user directory if it's now empty
	_ = os.Remove(filepath.Join(c.storagePath, req.Username))

	c.logger.Printf("Deleted %d/%d chunks for user %s", deletedCount, len(req.ChunkIds), req.Username)
	return &dfspb.DeleteChunksResponse{
		Success:      true,
		DeletedCount: deletedCount,
	}, nil
}

// resolveActiveMaster returns the best known master address.
// In a LAN environment, we always prefer the explicit flag provided at startup
// unless we're in failover mode.
func resolveActiveMaster(configuredPrimary, configuredSecondary string) string {
	// A chunkserver on a remote LAN device should rely on the -master flag.
	// Only fall back to .master_addr if the primary is unknown and we need
	// dynamic re-discovery of a promoted secondary.
	if configuredPrimary != "" && !strings.Contains(configuredPrimary, "127.0.0.1") && !strings.Contains(configuredPrimary, "localhost") {
		return configuredPrimary
	}

	// Dynamic fallback for failover or local dev
	if data, err := os.ReadFile(".master_addr"); err == nil {
		if addr := strings.TrimSpace(string(data)); addr != "" {
			return addr
		}
	}

	if configuredPrimary != "" {
		return configuredPrimary
	}

	return config.GetMasterAddr()
}

// SendHeartbeats periodically pings the master. If the primary is unreachable,
// it attempts to find an active master by checking the secondary address.
func SendHeartbeats(port string, masterAddr string, secondaryAddr string, logger *log.Logger) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	myAddr := config.GetMyAddr(port)
	if secondaryAddr == "" {
		secondaryAddr = config.GetSecondaryMasterAddr()
	}

	var conn *grpc.ClientConn
	var masterClient dfspb.MasterServerClient
	currentTarget := ""

	// We'll alternate between primary and secondary if one fails
	targets := []string{masterAddr}
	if secondaryAddr != "" && secondaryAddr != masterAddr {
		targets = append(targets, secondaryAddr)
	}

	for range ticker.C {
		// If we don't have a working connection, resolve a target
		if masterClient == nil {
			target := resolveActiveMaster(masterAddr, secondaryAddr)
			if target == "" {
				logger.Printf("ERROR: No master address available for heartbeats")
				continue
			}

			// Try to connect (non-blocking, but the first RPC will fail if unreachable)
			c, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				logger.Printf("CRITICAL: Failed to create gRPC client for %s: %v", target, err)
				continue
			}

			conn = c
			masterClient = dfspb.NewMasterServerClient(conn)
			currentTarget = target
			logger.Printf("Attempting heartbeat connection to: %s", target)
		}

		// Send heartbeat
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err := masterClient.ReceiveHeartbeat(ctx, &dfspb.HeartbeatRequest{
			Address: myAddr,
		})
		cancel()

		if err != nil {
			logger.Printf("Heartbeat RPC failed to %s: %v. (MyAddr: %s)", currentTarget, err, myAddr)
			// Close and reset so we re-resolve master next time
			if conn != nil {
				conn.Close()
			}
			conn = nil
			masterClient = nil
			currentTarget = ""
		} else {
			// Success! Don't be too verbose unless debugging, but keep a record.
			// logger.Printf("Heartbeat Success: %s -> %s", myAddr, currentTarget)
		}
	}
}
