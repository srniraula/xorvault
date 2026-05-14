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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// scanInventory walks the storage directory and collects all chunk IDs.
// Verifies data integrity by checking checksums - corrupted chunks are deleted.
func (c *ChunkServer) scanInventory() []string {
	var chunkIDs []string
	corruptedCount := 0

	err := filepath.Walk(c.storagePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if strings.HasSuffix(info.Name(), ".checksum") {
			return nil
		}

		relPath, err := filepath.Rel(c.storagePath, path)
		if err != nil {
			return err
		}

		chunkID := filepath.Base(relPath)

		// Verify data integrity by checking checksum
		chunkData, err := os.ReadFile(path)
		if err != nil {
			c.logger.Printf("Failed to read chunk %s: %v", chunkID, err)
			return nil
		}

		calculatedChecksum := calculateChecksum(chunkData)
		checksumPath := path + ".checksum"
		storedChecksumBytes, err := os.ReadFile(checksumPath)
		if err != nil {
			c.logger.Printf("No checksum found for %s - deleting unverifiable chunk", chunkID)

			if err := os.Remove(path); err != nil {
				c.logger.Printf("Failed to delete chunk without checksum %s: %v", chunkID, err)
			}

			corruptedCount++
			return nil
		}

		storedChecksum := string(storedChecksumBytes)

		if calculatedChecksum != storedChecksum {
			c.logger.Printf("CORRUPTION DETECTED: %s (expected: %s, got: %s) - deleting",
				chunkID, storedChecksum, calculatedChecksum)

			if err := os.Remove(path); err != nil {
				c.logger.Printf("Failed to delete corrupted chunk %s: %v", chunkID, err)
			}

			os.Remove(checksumPath)
			corruptedCount++
			return nil
		}

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

// reportInventoryToMaster sends the current inventory to the active master.
func (c *ChunkServer) reportInventoryToMaster(port string, tracker *MasterTracker, addrOverride string) (*dfspb.InventoryResponse, error) {
	inventory := c.scanInventory()
	masterAddr := tracker.ActiveAddr()

	conn, err := grpc.NewClient(masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to master %s: %v", masterAddr, err)
	}
	defer conn.Close()

	masterClient := dfspb.NewMasterServerClient(conn)

	// Use explicit addr if provided, otherwise auto-detect
	myAddr := addrOverride
	if myAddr == "" {
		myAddr = config.GetMyAddr(port)
	}

	resp, err := masterClient.ReportInventory(context.Background(), &dfspb.InventoryRequest{
		Address:  myAddr,
		ChunkIds: inventory,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to report inventory to %s: %v", masterAddr, err)
	}

	c.logger.Printf("Inventory reported to master %s: %d missing, %d extra",
		masterAddr, len(resp.MissingChunks), len(resp.ExtraChunks))

	return resp, nil
}

// cleanupExtraChunks deletes orphaned chunks not recognized by master metadata.
func (c *ChunkServer) cleanupExtraChunks(extraChunks []string) {
	if len(extraChunks) == 0 {
		return
	}

	c.logger.Printf("Cleaning up %d orphaned chunks", len(extraChunks))

	for _, chunkID := range extraChunks {
		err := filepath.Walk(c.storagePath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if !info.IsDir() && filepath.Base(path) == chunkID {
				if err := os.Remove(path); err != nil {
					c.logger.Printf("Failed to delete %s: %v", chunkID, err)
					return nil
				}

				checksumPath := path + ".checksum"
				os.Remove(checksumPath)

				c.logger.Printf("Deleted orphaned chunk: %s", chunkID)
			}
			return nil
		})

		if err != nil {
			c.logger.Printf("Error during cleanup of %s: %v", chunkID, err)
		}
	}
}

// PerformInventoryCheck scans inventory and reports to master on startup.
func PerformInventoryCheck(server *ChunkServer, port string, tracker *MasterTracker, logger *log.Logger, addrOverride string) {
	logger.Printf("Starting inventory check...")

	resp, err := server.reportInventoryToMaster(port, tracker, addrOverride)
	if err != nil {
		logger.Printf("Inventory check failed: %v", err)
		return
	}

	if len(resp.ExtraChunks) > 0 {
		server.cleanupExtraChunks(resp.ExtraChunks)
	}

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
