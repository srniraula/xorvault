package main

import (
	"context"
	"dfs-project/dfspb"
)

// ReportInventory handles chunk inventory reports from chunk servers
// Compares reported inventory with expected chunks and identifies discrepancies.
// Extra (orphaned) chunks are cleaned up asynchronously via DeleteChunks RPC.
func (m *MasterServer) ReportInventory(ctx context.Context, req *dfspb.InventoryRequest) (*dfspb.InventoryResponse, error) {
	m.mu.Lock()

	addr := req.Address
	reportedChunks := make(map[string]bool)
	for _, chunkID := range req.ChunkIds {
		reportedChunks[chunkID] = true
	}

	m.logger.Printf("Received inventory from %s: %d chunks reported", addr, len(req.ChunkIds))

	// Build expected chunks for this server from fileInfo
	expectedChunks := make(map[string]bool)
	for _, cliendIDtoFiles := range m.fileInfo {
		for _, stripes := range cliendIDtoFiles {
			for _, stripe := range stripes {
				for i, server := range stripe.Servers {
					if server == addr {
						chunkID := stripe.ChunkIds[i]
						expectedChunks[chunkID] = true
					}
				}
			}
		}
	}

	m.logger.Printf("Expected chunks for %s: %d chunks", addr, len(expectedChunks))

	// Find missing chunks (expected but not reported)
	var missingChunks []string
	for chunkID := range expectedChunks {
		if !reportedChunks[chunkID] {
			missingChunks = append(missingChunks, chunkID)
		}
	}

	// Find extra chunks (reported but not expected - orphaned)
	var extraChunks []string
	for chunkID := range reportedChunks {
		if !expectedChunks[chunkID] {
			extraChunks = append(extraChunks, chunkID)
		}
	}

	m.logger.Printf("Inventory analysis for %s: %d missing, %d extra",
		addr, len(missingChunks), len(extraChunks))

	// Build reconstruction tasks for missing chunks
	var reconstructionTasks []*dfspb.ReconstructionTask
	if len(missingChunks) > 0 {
		reconstructionTasks = m.buildReconstructionTasks(missingChunks, addr)
		m.logger.Printf("Built %d reconstruction tasks for %s", len(reconstructionTasks), addr)
	}

	// Capture extraChunks snapshot so we can release lock before the gRPC call
	extraCopy := make([]string, len(extraChunks))
	copy(extraCopy, extraChunks)

	m.mu.Unlock() // Release lock before any network I/O

	// Clean up orphaned chunks asynchronously so the inventory response is not delayed.
	// clientId=0, username="" signals the chunkserver to do a wildcard directory search.
	if len(extraCopy) > 0 {
		go func() {
			m.logger.Printf("Cleaning up %d orphaned chunks on %s", len(extraCopy), addr)
			deleted, err := m.deleteChunksFromServer(addr, extraCopy, 0, "")
			if err != nil {
				m.logger.Printf("Orphan cleanup on %s failed: %v", addr, err)
			} else {
				m.logger.Printf("Orphan cleanup: deleted %d/%d extra chunks from %s", deleted, len(extraCopy), addr)
			}
		}()
	}

	return &dfspb.InventoryResponse{
		MissingChunks:       missingChunks,
		ExtraChunks:         extraChunks,
		ReconstructionTasks: reconstructionTasks,
	}, nil
}

// buildReconstructionTasks creates reconstruction metadata for missing chunks
// Returns tasks with info about which chunks to download and XOR
func (m *MasterServer) buildReconstructionTasks(missingChunks []string, recoveringServer string) []*dfspb.ReconstructionTask {
	var tasks []*dfspb.ReconstructionTask

	// For each missing chunk, find its stripe and build reconstruction task
	for _, missingChunkID := range missingChunks {
		task := m.buildTaskForChunk(missingChunkID, recoveringServer)
		if task != nil {
			tasks = append(tasks, task)
		}
	}

	return tasks
}

// buildTaskForChunk creates a single reconstruction task
// Finds the stripe, identifies the 2 available chunks, and returns metadata
func (m *MasterServer) buildTaskForChunk(missingChunkID string, recoveringServer string) *dfspb.ReconstructionTask {
	// Find which file and stripe this chunk belongs to
	for _, ClientIDtoFilenames := range m.fileInfo {
		for filename, stripes := range ClientIDtoFilenames {
			for stripeNum, stripe := range stripes {
				// Check if this chunk is in this stripe
				var missingIndex int = -1
				for i, chunkID := range stripe.ChunkIds {
					if chunkID == missingChunkID && stripe.Servers[i] == recoveringServer {
						missingIndex = i
						break
					}
				}

				if missingIndex == -1 {
					continue // Not in this stripe
				}

				// Found it! Build reconstruction task with the other 2 chunks
				var otherChunkIDs []string
				var otherServers []string
				var clientID int64

				// Get client ID from filename
				for cid, files := range m.clientIDs {
					for _, f := range files {
						if f == filename {
							clientID = cid
							goto found
						}
					}
				}
			found:

				// Look up username for directory naming
				username := m.clientUsernames[clientID]

				// Collect the other 2 chunks (not the missing one)
				for i := 0; i < 3; i++ {
					if i != missingIndex {
						otherChunkIDs = append(otherChunkIDs, stripe.ChunkIds[i])
						otherServers = append(otherServers, stripe.Servers[i])
					}
				}

				return &dfspb.ReconstructionTask{
					ChunkId:       missingChunkID,
					StripeNum:     stripeNum,
					OtherChunkIds: otherChunkIDs,
					OtherServers:  otherServers,
					ClientId:      clientID,
					Username:      username,
				}
			}
		}
	}

	m.logger.Printf("WARNING: Could not find stripe for missing chunk %s", missingChunkID)
	return nil
}
