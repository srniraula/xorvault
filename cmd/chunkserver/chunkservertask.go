package main

import (
	"context"
	"dfs-project/dfspb"
	"dfs-project/pkg/config"
	"fmt"
	"log"
	"os"
	"path/filepath"
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

	// Create client subdirectory: storagePath/client_id/
	clientDir := filepath.Join(c.storagePath, fmt.Sprintf("%d", req.ClientId))
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
	// Read from client subdirectory: storagePath/client_id/chunk_id
	clientDir := filepath.Join(c.storagePath, fmt.Sprintf("%d", req.ClientId))
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
// Batched deletion for efficiency
func (c *ChunkServer) DeleteChunks(ctx context.Context, req *dfspb.DeleteChunksRequest) (*dfspb.DeleteChunksResponse, error) {
	deletedCount := int32(0)

	// Delete each chunk in the batch
	for _, chunkID := range req.ChunkIds {
		// Construct path: storagePath/client_id/chunk_id
		clientDir := filepath.Join(c.storagePath, fmt.Sprintf("%d", req.ClientId))
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

	c.logger.Printf("Deleted %d/%d chunks for client %d", deletedCount, len(req.ChunkIds), req.ClientId)
	return &dfspb.DeleteChunksResponse{
		Success:      true,
		DeletedCount: deletedCount,
	}, nil
}

func SendHeartbeats(port string, masterAddr string, logger *log.Logger) {
	// returns a ticker object with a channel (ticker.C)
	// Every 5 seconds, the ticker sends the current time to its channel
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Determine which master address to use: prefer explicitly provided masterAddr,
	// otherwise fall back to configured/default master address.
	target := masterAddr
	if target == "" {
		target = config.GetMasterAddr()
	}

	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Printf("Failed to connect to master for heartbeat: %v", err)
		return
	}
	defer conn.Close()

	masterClient := dfspb.NewMasterServerClient(conn)
	myAddr := config.GetMyAddr(port)

	for range ticker.C {
		_, err := masterClient.ReceiveHeartbeat(context.Background(), &dfspb.HeartbeatRequest{
			Address: myAddr,
		})
		if err != nil {
			logger.Printf("Heartbeat failed: %v", err)
		} else {
			logger.Printf("Heartbeat sent to master from %s", myAddr)
		}
	}
}
