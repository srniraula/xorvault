package main

import (
	"context"
	"dfs-project/dfspb"
	"fmt"
	"math"
	"os"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// globalReconstrNs accumulates RAID-4 reconstruction (XOR) time in nanoseconds.
// Drained by download() via DrainReconstrMs() after all stripes are processed.
var globalReconstrNs int64

// DrainReconstrMs returns accumulated reconstruction time in ms and resets the counter.
func DrainReconstrMs() float64 {
	ns := atomic.SwapInt64(&globalReconstrNs, 0)
	return math.Round(float64(ns)/1e4) / 100.0
}

// DownloadedChunk represents the result of downloading a single chunk
type DownloadedChunk struct {
	ChunkID  string
	Data     []byte
	Success  bool
	Error    error
	IsData1  bool // true if this is first data chunk in stripe
	IsData2  bool // true if this is second data chunk in stripe
	IsParity bool // true if this is parity chunk
}

// StripeDownload holds downloaded chunks for a single stripe
type StripeDownload struct {
	StripeNum   int
	DataChunk1  []byte // nil if needs reconstruction
	DataChunk2  []byte // nil if needs reconstruction
	ParityChunk []byte // nil if not downloaded
	ChunksOK    int    // Count of successfully downloaded chunks (0-3)
}

// DownloadStripeInfo maps stripe metadata from master's chunk allocation
type DownloadStripeInfo struct {
	StripeNum   int
	DataChunk1  ChunkServerPair // chunk ID + server address
	DataChunk2  ChunkServerPair
	ParityChunk ChunkServerPair
}

// ChunkServerPair holds chunk ID and its server location
type ChunkServerPair struct {
	ChunkID string
	Server  string
}

// downloadChunkFromServer downloads a single chunk from its designated server.
func downloadChunkFromServer(chunkID, serverAddr string, clientID int64, isData1, isData2, isParity bool) DownloadedChunk {
	result := DownloadedChunk{
		ChunkID:  chunkID,
		IsData1:  isData1,
		IsData2:  isData2,
		IsParity: isParity,
	}

	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		result.Success = false
		result.Error = fmt.Errorf("connection failed: %v", err)
		return result
	}
	defer conn.Close()

	client := dfspb.NewChunkServerClient(conn)

	// Request chunk data with client ID for physical isolation
	resp, err := client.ReadChunk(context.Background(), &dfspb.ReadChunkRequest{
		ChunkId:  chunkID,
		ClientId: clientID,
	})

	if err != nil {
		result.Success = false
		result.Error = fmt.Errorf("ReadChunk RPC failed: %v", err)
		return result
	}

	// Verify checksum if server provided one
	if resp.Checksum != "" {
		localChecksum := calculateChecksum(resp.Data)
		if localChecksum != resp.Checksum {
			result.Success = false
			result.Error = fmt.Errorf("checksum mismatch: expected %s, got %s", resp.Checksum, localChecksum)
			return result
		}
	}

	result.Data = resp.Data
	result.Success = true
	return result
}

// downloadStripe downloads all 3 chunks of a stripe in parallel.
// Returns StripeDownload with downloaded chunks and success count.
func downloadStripe(stripeInfo DownloadStripeInfo, clientID int64) StripeDownload {
	var wg sync.WaitGroup
	resultChan := make(chan DownloadedChunk, 3)

	// Download data chunk 1 (if exists)
	if stripeInfo.DataChunk1.ChunkID != "" && stripeInfo.DataChunk1.Server != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := downloadChunkFromServer(
				stripeInfo.DataChunk1.ChunkID,
				stripeInfo.DataChunk1.Server,
				clientID,
				true, false, false,
			)
			resultChan <- result
		}()
	}

	// Download data chunk 2 (if exists - may be missing in last stripe)
	if stripeInfo.DataChunk2.ChunkID != "" && stripeInfo.DataChunk2.Server != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := downloadChunkFromServer(
				stripeInfo.DataChunk2.ChunkID,
				stripeInfo.DataChunk2.Server,
				clientID,
				false, true, false,
			)
			resultChan <- result
		}()
	}

	// Download parity chunk (if exists)
	if stripeInfo.ParityChunk.ChunkID != "" && stripeInfo.ParityChunk.Server != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := downloadChunkFromServer(
				stripeInfo.ParityChunk.ChunkID,
				stripeInfo.ParityChunk.Server,
				clientID,
				false, false, true,
			)
			resultChan <- result
		}()
	}

	wg.Wait()
	close(resultChan)

	stripe := StripeDownload{
		StripeNum: stripeInfo.StripeNum,
	}

	for result := range resultChan {
		if result.Success {
			stripe.ChunksOK++
			if result.IsData1 {
				stripe.DataChunk1 = result.Data
			} else if result.IsData2 {
				stripe.DataChunk2 = result.Data
			} else if result.IsParity {
				stripe.ParityChunk = result.Data
			}
		}
	}

	return stripe
}

// reconstructMissingChunk uses XOR to recover missing chunk from available 2 chunks.
// Reconstruction time is accumulated into globalReconstrNs for metrics reporting.
func reconstructMissingChunk(stripe *StripeDownload, stripeInfo DownloadStripeInfo) error {

	dataChunksExpected := 0
	if stripeInfo.DataChunk1.ChunkID != "" {
		dataChunksExpected++
	}
	if stripeInfo.DataChunk2.ChunkID != "" {
		dataChunksExpected++
	}

	dataChunksAvailable := 0
	if stripe.DataChunk1 != nil {
		dataChunksAvailable++
	}
	if stripe.DataChunk2 != nil {
		dataChunksAvailable++
	}

	// SPECIAL CASE: all expected data chunks present (handles odd-chunk last stripe)
	if dataChunksAvailable == dataChunksExpected && dataChunksAvailable > 0 {
		return nil
	}

	// Case A: All 3 chunks available - no reconstruction needed
	if stripe.ChunksOK == 3 {
		return nil
	}

	// Case C: Less than 2 chunks total - cannot reconstruct
	if stripe.ChunksOK < 2 {
		return fmt.Errorf("insufficient chunks for reconstruction: only %d available", stripe.ChunksOK)
	}

	// Case B: Exactly 2 chunks — reconstruct the missing one using XOR.
	// Time the XOR and accumulate into globalReconstrNs.

	// Missing data chunk 1: data1 = data2 XOR parity
	if stripe.DataChunk1 == nil && stripe.DataChunk2 != nil && stripe.ParityChunk != nil {
		t := newPhaseTimer()
		stripe.DataChunk1 = calculateParity(stripe.DataChunk2, stripe.ParityChunk)
		atomic.AddInt64(&globalReconstrNs, int64(t.ElapsedMs()*1e6))
		return nil
	}

	// Missing data chunk 2: data2 = data1 XOR parity
	if stripe.DataChunk2 == nil && stripe.DataChunk1 != nil && stripe.ParityChunk != nil {
		t := newPhaseTimer()
		stripe.DataChunk2 = calculateParity(stripe.DataChunk1, stripe.ParityChunk)
		atomic.AddInt64(&globalReconstrNs, int64(t.ElapsedMs()*1e6))
		return nil
	}

	// Missing parity only — not needed for output, no reconstruction required
	if stripe.ParityChunk == nil && stripe.DataChunk1 != nil && stripe.DataChunk2 != nil {
		return nil
	}

	return fmt.Errorf("unexpected chunk combination for reconstruction")
}

// writeStripeToFile writes stripe data to output file, handling padding removal.
func writeStripeToFile(file *os.File, stripe *StripeDownload, isLastStripe bool, originalFileSize int64, bytesWritten int64) (int, error) {
	totalWritten := 0

	if stripe.DataChunk1 != nil {
		data := stripe.DataChunk1

		// Remove padding from last chunk if needed
		if isLastStripe {
			remainingBytes := originalFileSize - bytesWritten
			if remainingBytes < int64(len(data)) {
				data = data[:remainingBytes]
			}
		}

		n, err := file.Write(data)
		if err != nil {
			return totalWritten, fmt.Errorf("failed to write data chunk 1: %v", err)
		}
		totalWritten += n
		bytesWritten += int64(n)
	}

	if stripe.DataChunk2 != nil {
		data := stripe.DataChunk2

		// Remove padding from last chunk if needed
		if isLastStripe {
			remainingBytes := originalFileSize - bytesWritten
			if remainingBytes < int64(len(data)) {
				data = data[:remainingBytes]
			}
		}

		n, err := file.Write(data)
		if err != nil {
			return totalWritten, fmt.Errorf("failed to write data chunk 2: %v", err)
		}
		totalWritten += n
	}

	// Parity chunk is NOT written to output file

	return totalWritten, nil
}
