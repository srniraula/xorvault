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

// SendHeartbeats periodically pings the master. If the primary is unreachable,
// it attempts to find an active master by actively probing ALL known addresses.
// This is critical for LAN deployments where .master_addr is local to each device.
func SendHeartbeats(port string, masterAddr string, secondaryAddr string, logger *log.Logger) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	myAddr := config.GetMyAddr(port)
	if secondaryAddr == "" {
		secondaryAddr = config.GetSecondaryMasterAddr()
	}

	var conn *grpc.ClientConn
	var masterClient dfspb.MasterServerClient
	currentTarget := ""

	// Build all known master addresses (used for active probing on failover)
	var allMasters []string
	if masterAddr != "" {
		allMasters = append(allMasters, masterAddr)
	}
	if secondaryAddr != "" && secondaryAddr != masterAddr {
		allMasters = append(allMasters, secondaryAddr)
	}
	// Also add config default if different
	configMaster := config.GetMasterAddr()
	if configMaster != "" && configMaster != masterAddr && configMaster != secondaryAddr {
		allMasters = append(allMasters, configMaster)
	}

	for range ticker.C {
		// If we don't have a working connection, actively probe ALL known masters
		if masterClient == nil {
			// Build probe list: start with .master_addr hint, then try all known masters
			var probeTargets []string

<<<<<<< Updated upstream
			// 1. Try dynamic .master_addr first (failover detection)
=======
			// Check .master_addr for a hint (useful when running on same machine as master)
>>>>>>> Stashed changes
			if data, err := os.ReadFile(".master_addr"); err == nil {
				if addr := strings.TrimSpace(string(data)); addr != "" {
					probeTargets = append(probeTargets, addr)
				}
			}

<<<<<<< Updated upstream
			// 2. Otherwise cycle through explicitly defined primary and secondary
			if target == "" && len(targets) > 0 {
				target = targets[targetIdx]
			}

			// 3. Fallback to cluster default
			if target == "" {
				target = config.GetMasterAddr()
			}

			if target == "" {
				logger.Printf("ERROR: No master address available")
				continue
			}

			// grpc.NewClient is non-blocking
			c, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				logger.Printf("Failed to create client for %s: %v", target, err)
				continue
			}

			conn = c
			masterClient = dfspb.NewMasterServerClient(conn)
			currentTarget = target
		}

		// Send heartbeat
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
=======
			// Add all known masters (avoiding duplicates)
			for _, addr := range allMasters {
				duplicate := false
				for _, existing := range probeTargets {
					if existing == addr {
						duplicate = true
						break
					}
				}
				if !duplicate {
					probeTargets = append(probeTargets, addr)
				}
			}

			if len(probeTargets) == 0 {
				logger.Printf("ERROR: No master addresses available for heartbeats")
				continue
			}

			// Actively probe each target until we find a working one
			var foundConn *grpc.ClientConn
			var foundClient dfspb.MasterServerClient
			var foundTarget string

			for _, target := range probeTargets {
				c, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
				if err != nil {
					logger.Printf("Failed to create gRPC client for %s: %v", target, err)
					continue
				}

				// Actually test the connection with a quick heartbeat
				testClient := dfspb.NewMasterServerClient(c)
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_, err = testClient.ReceiveHeartbeat(ctx, &dfspb.HeartbeatRequest{Address: myAddr})
				cancel()

				if err != nil {
					logger.Printf("Probe failed for %s: %v", target, err)
					c.Close()
					continue
				}

				// Found a working master!
				foundConn = c
				foundClient = testClient
				foundTarget = target
				logger.Printf("Connected to active master: %s", target)
				break
			}

			if foundClient == nil {
				logger.Printf("All master probes failed. Will retry in 2s. Targets: %v", probeTargets)
				continue
			}

			conn = foundConn
			masterClient = foundClient
			currentTarget = foundTarget
			continue // Already sent heartbeat during probe
		}

		// Send heartbeat to current master
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
>>>>>>> Stashed changes
		_, err := masterClient.ReceiveHeartbeat(ctx, &dfspb.HeartbeatRequest{
			Address: myAddr,
		})
		cancel()

		if err != nil {
<<<<<<< Updated upstream
			logger.Printf("Heartbeat failed to %s: %v", currentTarget, err)
=======
			logger.Printf("Heartbeat failed to %s: %v. Will probe all masters.", currentTarget, err)
			// Close and reset - next iteration will probe all known masters
>>>>>>> Stashed changes
			if conn != nil {
				conn.Close()
			}
			conn = nil
			masterClient = nil
			currentTarget = ""
<<<<<<< Updated upstream
			
			// Increment index for the next ticker tick to try the alternate master
			if len(targets) > 0 {
				targetIdx = (targetIdx + 1) % len(targets)
			}
=======
>>>>>>> Stashed changes
		}
	}
}
