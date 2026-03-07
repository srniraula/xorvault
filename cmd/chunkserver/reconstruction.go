package main

import (
	"context"
	"dfs-project/dfspb"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// reconstructChunks processes reconstruction tasks from master
// Downloads chunks from peers, XORs them, and stores reconstructed chunks
func (c *ChunkServer) reconstructChunks(tasks []*dfspb.ReconstructionTask) error {
	if len(tasks) == 0 {
		return nil
	}

	c.logger.Printf("Starting reconstruction of %d chunks", len(tasks))

	successCount := 0
	for _, task := range tasks {
		err := c.reconstructSingleChunk(task)
		if err != nil {
			c.logger.Printf("Failed to reconstruct %s: %v", task.ChunkId, err)
			continue
		}
		successCount++
	}

	c.logger.Printf("Reconstruction complete: %d/%d chunks restored", successCount, len(tasks))

	if successCount < len(tasks) {
		return fmt.Errorf("partial reconstruction: %d/%d failed", len(tasks)-successCount, len(tasks))
	}

	return nil
}

// reconstructSingleChunk handles reconstruction of one chunk
// Downloads 2 chunks from peers in parallel, XORs them, stores result
func (c *ChunkServer) reconstructSingleChunk(task *dfspb.ReconstructionTask) error {
	c.logger.Printf("Reconstructing %s (stripe %d)", task.ChunkId, task.StripeNum)

	// Download the 2 available chunks in parallel using goroutines
	type downloadResult struct {
		data     []byte
		checksum string
		err      error
	}

	results := make([]downloadResult, 2)
	done := make(chan int, 2)

	// Goroutine 1: Download first chunk
	go func() {
		data, checksum, err := c.downloadChunkFromPeer(task.OtherServers[0], task.OtherChunkIds[0], task.ClientId, task.Username)
		results[0] = downloadResult{data: data, checksum: checksum, err: err}
		done <- 0
	}()

	// Goroutine 2: Download second chunk
	go func() {
		data, checksum, err := c.downloadChunkFromPeer(task.OtherServers[1], task.OtherChunkIds[1], task.ClientId, task.Username)
		results[1] = downloadResult{data: data, checksum: checksum, err: err}
		done <- 1
	}()

	// Wait for both downloads to complete
	<-done
	<-done

	// Check for download errors
	if results[0].err != nil {
		return fmt.Errorf("failed to download %s from %s: %v", task.OtherChunkIds[0], task.OtherServers[0], results[0].err)
	}
	if results[1].err != nil {
		return fmt.Errorf("failed to download %s from %s: %v", task.OtherChunkIds[1], task.OtherServers[1], results[1].err)
	}

	// Verify checksums in parallel
	type checksumResult struct {
		index int
		err   error
	}
	checksumDone := make(chan checksumResult, 2)

	go func() {
		var err error
		if results[0].checksum != "" && calculateChecksum(results[0].data) != results[0].checksum {
			err = fmt.Errorf("checksum mismatch for %s", task.OtherChunkIds[0])
		}
		checksumDone <- checksumResult{index: 0, err: err}
	}()

	go func() {
		var err error
		if results[1].checksum != "" && calculateChecksum(results[1].data) != results[1].checksum {
			err = fmt.Errorf("checksum mismatch for %s", task.OtherChunkIds[1])
		}
		checksumDone <- checksumResult{index: 1, err: err}
	}()

	// Wait for both checksum verifications
	for i := 0; i < 2; i++ {
		result := <-checksumDone
		if result.err != nil {
			return result.err
		}
	}

	chunk1 := results[0].data
	chunk2 := results[1].data

	// XOR to reconstruct missing chunk
	reconstructed := xorBytes(chunk1, chunk2)

	// Calculate checksum for reconstructed chunk
	reconstructedChecksum := calculateChecksum(reconstructed)

	// Store reconstructed chunk (reuse WriteChunk)
	_, err := c.WriteChunk(context.Background(), &dfspb.WriteChunkRequest{
		ChunkId:  task.ChunkId,
		Data:     reconstructed,
		Checksum: reconstructedChecksum,
		ClientId: task.ClientId,
		Username: task.Username,
	})
	if err != nil {
		return fmt.Errorf("failed to store reconstructed chunk: %v", err)
	}

	c.logger.Printf("Successfully reconstructed %s (%d bytes, checksum: %s)",
		task.ChunkId, len(reconstructed), reconstructedChecksum)

	return nil
}

// downloadChunkFromPeer downloads a chunk from another chunk server
// Reuses the ReadChunk RPC that already exists
func (c *ChunkServer) downloadChunkFromPeer(serverAddr string, chunkID string, clientID int64, username string) ([]byte, string, error) {
	// Connect to peer chunk server
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, "", fmt.Errorf("failed to connect to %s: %v", serverAddr, err)
	}
	defer conn.Close()

	peerClient := dfspb.NewChunkServerClient(conn)

	// Use existing ReadChunk RPC
	resp, err := peerClient.ReadChunk(context.Background(), &dfspb.ReadChunkRequest{
		ChunkId:  chunkID,
		ClientId: clientID,
		Username: username,
	})
	if err != nil {
		return nil, "", fmt.Errorf("ReadChunk RPC failed: %v", err)
	}

	c.logger.Printf("Downloaded %s from %s (%d bytes)", chunkID, serverAddr, len(resp.Data))

	return resp.Data, resp.Checksum, nil
}

// xorBytes performs XOR operation on two byte slices
// Reuses parity calculation logic from client
func xorBytes(a, b []byte) []byte {
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}

	result := make([]byte, maxLen)

	for i := 0; i < maxLen; i++ {
		var byteA, byteB byte
		if i < len(a) {
			byteA = a[i]
		}
		if i < len(b) {
			byteB = b[i]
		}
		result[i] = byteA ^ byteB
	}

	return result
}
