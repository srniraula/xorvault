// package main

// import (
// 	"context"
// 	"fmt"
// 	"log"
// 	"os"
// 	"path/filepath"
// 	"strings"

// 	"dfs-project/dfspb"

// 	"google.golang.org/grpc"
// 	"google.golang.org/grpc/credentials/insecure"
// )

// // scanInventory walks the storage directory and collects all chunk IDs
// // Returns a list of chunk IDs that this server currently has on disk
// // Verifies data integrity by checking checksums - corrupted chunks are deleted
// func (c *ChunkServer) scanInventory() []string {
// 	var chunkIDs []string
// 	corruptedCount := 0

// 	// Recursively visits every file and directory inside storagePath e.g. chunk_server1
// 	err := filepath.Walk(c.storagePath, func(path string, info os.FileInfo, err error) error {
// 		if err != nil {
// 			return err
// 		}

// 		// Skip directories
// 		if info.IsDir() {
// 			return nil
// 		}

// 		// Skip checksum files
// 		if strings.HasSuffix(info.Name(), ".checksum") {
// 			return nil
// 		}

// 		// Extract chunk ID from path
// 		// Path format: storagePath/client_id/chunk_id
// 		// relPath : client_id/chunk_id
// 		relPath, err := filepath.Rel(c.storagePath, path)
// 		if err != nil {
// 			return err
// 		}

// 		// Get the filename (chunk ID)
// 		chunkID := filepath.Base(relPath) // just chunk_id

// 		// Verify data integrity by checking checksum
// 		chunkData, err := os.ReadFile(path)
// 		if err != nil {
// 			c.logger.Printf("Failed to read chunk %s: %v", chunkID, err)
// 			return nil
// 		}

// 		// Calculate checksum
// 		calculatedChecksum := calculateChecksum(chunkData)

// 		// Read stored checksum
// 		checksumPath := path + ".checksum"
// 		storedChecksumBytes, err := os.ReadFile(checksumPath)
// 		if err != nil {
// 			// No checksum file - discard chunk
// 			c.logger.Printf("No checksum found for %s - deleting unverifiable chunk", chunkID)

// 			if err := os.Remove(path); err != nil {
// 				c.logger.Printf("Failed to delete chunk without checksum %s: %v", chunkID, err)
// 			}

// 			corruptedCount++
// 			return nil
// 		}

// 		storedChecksum := string(storedChecksumBytes)

// 		// Verify checksum
// 		if calculatedChecksum != storedChecksum {
// 			// Checksum mismatch - chunk is corrupted, delete it
// 			c.logger.Printf("CORRUPTION DETECTED: %s (expected: %s, got: %s) - deleting",
// 				chunkID, storedChecksum, calculatedChecksum)

// 			// Delete corrupted chunk
// 			if err := os.Remove(path); err != nil {
// 				c.logger.Printf("Failed to delete corrupted chunk %s: %v", chunkID, err)
// 			}

// 			// Delete checksum file
// 			os.Remove(checksumPath)

// 			corruptedCount++
// 			return nil
// 		}

// 		// Checksum matches - chunk is valid
// 		chunkIDs = append(chunkIDs, chunkID)

// 		return nil
// 	})

// 	if err != nil {
// 		c.logger.Printf("Error scanning inventory: %v", err)
// 		return chunkIDs
// 	}

// 	c.logger.Printf("Inventory scan complete: found %d valid chunks, deleted %d corrupted chunks",
// 		len(chunkIDs), corruptedCount)
// 	return chunkIDs
// }

// // reportInventoryToMaster sends the current inventory to the master
// // Returns lists of missing and extra chunks
// func (c *ChunkServer) reportInventoryToMaster(port string) (*dfspb.InventoryResponse, error) {
// 	// Scan local inventory
// 	inventory := c.scanInventory()

// 	// Connect to master
// 	conn, err := grpc.NewClient("127.0.0.1:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to connect to master: %v", err)
// 	}
// 	defer conn.Close()

// 	masterClient := dfspb.NewMasterServerClient(conn)
// 	myAddr := fmt.Sprintf("127.0.0.1:%s", port)

// 	// Report inventory
// 	resp, err := masterClient.ReportInventory(context.Background(), &dfspb.InventoryRequest{
// 		Address:  myAddr,
// 		ChunkIds: inventory,
// 	})

// 	if err != nil {
// 		return nil, fmt.Errorf("failed to report inventory: %v", err)
// 	}

// 	c.logger.Printf("Inventory reported to master: %d missing, %d extra",
// 		len(resp.MissingChunks), len(resp.ExtraChunks))

// 	return resp, nil
// }

// // cleanupExtraChunks deletes orphaned chunks not in master's metadata
// func (c *ChunkServer) cleanupExtraChunks(extraChunks []string) {
// 	if len(extraChunks) == 0 {
// 		return
// 	}

// 	c.logger.Printf("Cleaning up %d orphaned chunks", len(extraChunks))

// 	for _, chunkID := range extraChunks {
// 		// Find and delete the chunk file
// 		err := filepath.Walk(c.storagePath, func(path string, info os.FileInfo, err error) error {
// 			if err != nil {
// 				return err
// 			}

// 			if !info.IsDir() && filepath.Base(path) == chunkID {
// 				// Delete chunk file
// 				if err := os.Remove(path); err != nil {
// 					c.logger.Printf("Failed to delete %s: %v", chunkID, err)
// 					return nil
// 				}

// 				// Delete checksum file if exists
// 				checksumPath := path + ".checksum"
// 				os.Remove(checksumPath) // Ignore error if doesn't exist

// 				c.logger.Printf("Deleted orphaned chunk: %s", chunkID)
// 			}
// 			return nil
// 		})

// 		if err != nil {
// 			c.logger.Printf("Error during cleanup of %s: %v", chunkID, err)
// 		}
// 	}
// }

// // PerformInventoryCheck scans inventory and reports to master on startup
// // Called once when chunk server starts
// func PerformInventoryCheck(server *ChunkServer, port string, logger *log.Logger) {
// 	logger.Printf("Starting inventory check...")

// 	resp, err := server.reportInventoryToMaster(port)
// 	if err != nil {
// 		logger.Printf("Inventory check failed: %v", err)
// 		return
// 	}

// 	// Cleanup orphaned chunks
// 	if len(resp.ExtraChunks) > 0 {
// 		server.cleanupExtraChunks(resp.ExtraChunks)
// 	}

// 	// Reconstruct missing chunks
// 	if len(resp.ReconstructionTasks) > 0 {
// 		logger.Printf("Reconstructing %d missing chunks...", len(resp.ReconstructionTasks))
// 		err := server.reconstructChunks(resp.ReconstructionTasks)
// 		if err != nil {
// 			logger.Printf("Reconstruction error: %v", err)
// 		}
// 	} else if len(resp.MissingChunks) > 0 {
// 		logger.Printf("WARNING: %d chunks missing but no reconstruction tasks provided", len(resp.MissingChunks))
// 		for _, chunkID := range resp.MissingChunks {
// 			logger.Printf("  Missing: %s", chunkID)
// 		}
// 	} else {
// 		logger.Printf("Inventory check complete: all chunks present")
// 	}
// }

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"dfs-project/dfspb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// scanInventory walks the storage directory and collects all chunk IDs
// Returns a list of chunk IDs that this server currently has on disk
// Verifies data integrity by checking checksums - corrupted chunks are deleted
func (c *ChunkServer) scanInventory() []string {
	var chunkIDs []string
	corruptedCount := 0

	// Recursively visits every file and directory inside storagePath e.g. chunk_server1
	err := filepath.Walk(c.storagePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Skip checksum files
		if strings.HasSuffix(info.Name(), ".checksum") {
			return nil
		}

		// Extract chunk ID from path
		// Path format: storagePath/client_id/chunk_id
		// relPath : client_id/chunk_id
		relPath, err := filepath.Rel(c.storagePath, path)
		if err != nil {
			return err
		}

		// Get the filename (chunk ID)
		chunkID := filepath.Base(relPath) // just chunk_id

		// Verify data integrity by checking checksum
		chunkData, err := os.ReadFile(path)
		if err != nil {
			c.logger.Printf("Failed to read chunk %s: %v", chunkID, err)
			return nil
		}

		// Calculate checksum
		calculatedChecksum := calculateChecksum(chunkData)

		// Read stored checksum
		checksumPath := path + ".checksum"
		storedChecksumBytes, err := os.ReadFile(checksumPath)
		if err != nil {
			// No checksum file - discard chunk
			c.logger.Printf("No checksum found for %s - deleting unverifiable chunk", chunkID)

			if err := os.Remove(path); err != nil {
				c.logger.Printf("Failed to delete chunk without checksum %s: %v", chunkID, err)
			}

			corruptedCount++
			return nil
		}

		storedChecksum := string(storedChecksumBytes)

		// Verify checksum
		if calculatedChecksum != storedChecksum {
			// Checksum mismatch - chunk is corrupted, delete it
			c.logger.Printf("CORRUPTION DETECTED: %s (expected: %s, got: %s) - deleting",
				chunkID, storedChecksum, calculatedChecksum)

			// Delete corrupted chunk
			if err := os.Remove(path); err != nil {
				c.logger.Printf("Failed to delete corrupted chunk %s: %v", chunkID, err)
			}

			// Delete checksum file
			os.Remove(checksumPath)

			corruptedCount++
			return nil
		}

		// Checksum matches - chunk is valid
		chunkIDs = append(chunkIDs, chunkID)

		return nil
	})

	if err != nil {
		c.logger.Printf("Error scanning inventory: %v", err)
		return chunkIDs
	}

	c.logger.Printf("Inventory scan complete: found %d valid chunks, deleted %d corrupted chunks",
		len(chunkIDs), corruptedCount)
	return chunkIDs
}

// reportInventoryToMaster sends the current inventory to the master
// Returns lists of missing and extra chunks
// Updated to accept localIP, port, and masterAddr
func (c *ChunkServer) reportInventoryToMaster(localIP, port, masterAddr string) (*dfspb.InventoryResponse, error) {
	// Scan local inventory
	inventory := c.scanInventory()

	// Connect to master using provided address
	conn, err := grpc.NewClient(masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to master: %v", err)
	}
	defer conn.Close()

	masterClient := dfspb.NewMasterServerClient(conn)
	myAddr := fmt.Sprintf("%s:%s", localIP, port)

	// Report inventory
	resp, err := masterClient.ReportInventory(context.Background(), &dfspb.InventoryRequest{
		Address:  myAddr,
		ChunkIds: inventory,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to report inventory: %v", err)
	}

	c.logger.Printf("Inventory reported to master: %d missing, %d extra",
		len(resp.MissingChunks), len(resp.ExtraChunks))

	return resp, nil
}

// cleanupExtraChunks deletes orphaned chunks not in master's metadata
func (c *ChunkServer) cleanupExtraChunks(extraChunks []string) {
	if len(extraChunks) == 0 {
		return
	}

	c.logger.Printf("Cleaning up %d orphaned chunks", len(extraChunks))

	for _, chunkID := range extraChunks {
		// Find and delete the chunk file
		err := filepath.Walk(c.storagePath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if !info.IsDir() && filepath.Base(path) == chunkID {
				// Delete chunk file
				if err := os.Remove(path); err != nil {
					c.logger.Printf("Failed to delete %s: %v", chunkID, err)
					return nil
				}

				// Delete checksum file if exists
				checksumPath := path + ".checksum"
				os.Remove(checksumPath) // Ignore error if doesn't exist

				c.logger.Printf("Deleted orphaned chunk: %s", chunkID)
			}
			return nil
		})

		if err != nil {
			c.logger.Printf("Error during cleanup of %s: %v", chunkID, err)
		}
	}
}

// PerformInventoryCheck scans inventory and reports to master on startup
// Called once when chunk server starts
// Updated to accept localIP, port, and masterAddr
func PerformInventoryCheck(server *ChunkServer, localIP, port, masterAddr string, logger *log.Logger) {
	logger.Printf("Starting inventory check...")

	resp, err := server.reportInventoryToMaster(localIP, port, masterAddr)
	if err != nil {
		logger.Printf("Inventory check failed: %v", err)
		return
	}

	// Cleanup orphaned chunks
	if len(resp.ExtraChunks) > 0 {
		server.cleanupExtraChunks(resp.ExtraChunks)
	}

	// Reconstruct missing chunks
	if len(resp.ReconstructionTasks) > 0 {
		logger.Printf("Reconstructing %d missing chunks...", len(resp.ReconstructionTasks))
		err := server.reconstructChunks(resp.ReconstructionTasks)
		if err != nil {
			logger.Printf("Reconstruction error: %v", err)
		}
	} else if len(resp.MissingChunks) > 0 {
		logger.Printf("WARNING: %d chunks missing but no reconstruction tasks provided", len(resp.MissingChunks))
		for _, chunkID := range resp.MissingChunks {
			logger.Printf("  Missing: %s", chunkID)
		}
	} else {
		logger.Printf("Inventory check complete: all chunks present")
	}
}
