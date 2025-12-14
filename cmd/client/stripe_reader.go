package main

import (
	"dfs-project/dfspb"
	"fmt"
	"io"
	"os"
)

// streamFileInStripes reads a file and streams RAID-4 stripes via a channel
// Each stripe contains 2 data chunks and calculates parity
// Stripes are sent to channel as they're read - producer pattern for memory efficiency
// Caller must read from returned channel until closed
func streamFileInStripes(filePath string, stripeMap map[int32]*dfspb.StripeMetadata, stripeChan chan<- Stripe, errChan chan<- error) {
	defer close(stripeChan)
	defer close(errChan)
	file, err := os.Open(filePath)
	if err != nil {
		errChan <- fmt.Errorf("failed to open file: %v", err)
		return
	}
	defer file.Close()

	stripeNum := int32(1)

	for {
		// Read first chunk for this stripe
		chunk1 := make([]byte, CHUNK_SIZE)
		n1, err1 := io.ReadFull(file, chunk1)
		if err1 == io.EOF {
			// No more data
			break
		}
		if err1 != nil && err1 != io.ErrUnexpectedEOF {
			errChan <- fmt.Errorf("error reading chunk 1 of stripe %d: %v", stripeNum, err1)
			return
		}
		chunk1 = chunk1[:n1]

		// Read second chunk for this stripe
		chunk2 := make([]byte, CHUNK_SIZE)
		n2, err2 := io.ReadFull(file, chunk2)

		// Handle last stripe with only 1 chunk
		if err2 == io.EOF || n2 == 0 {
			chunk2 = padChunk([]byte{}, CHUNK_SIZE)
		} else if err2 != nil && err2 != io.ErrUnexpectedEOF {
			errChan <- fmt.Errorf("error reading chunk 2 of stripe %d: %v", stripeNum, err2)
			return
		} else {
			chunk2 = chunk2[:n2]
			if n2 < CHUNK_SIZE {
				chunk2 = padChunk(chunk2, CHUNK_SIZE)
			}
		}

		// Ensure chunk1 is also padded if needed
		if n1 < CHUNK_SIZE {
			chunk1 = padChunk(chunk1, CHUNK_SIZE)
		}

		// Calculate parity for this stripe
		parity := calculateParity(chunk1, chunk2)

		// Get chunk IDs and servers for this stripe from allocation map
		stripeInfo, exists := stripeMap[stripeNum]
		if !exists {
			errChan <- fmt.Errorf("no allocation found for stripe %d", stripeNum)
			return
		}

		// Calculate checksums
		checksum1 := calculateChecksum(chunk1)
		checksum2 := calculateChecksum(chunk2)
		checksumParity := calculateChecksum(parity)

		// Build stripe struct
		stripe := Stripe{
			StripeNum:   int(stripeNum),
			DataChunk1:  chunk1,
			DataChunk2:  chunk2,
			ParityChunk: parity,
			ChunkIDs:    [3]string{stripeInfo.ChunkIds[0], stripeInfo.ChunkIds[1], stripeInfo.ChunkIds[2]},
			Checksums:   [3]string{checksum1, checksum2, checksumParity},
			Servers:     [3]string{stripeInfo.Servers[0], stripeInfo.Servers[1], stripeInfo.Servers[2]},
		}

		// Send stripe to channel (blocks if buffer full - backpressure)
		stripeChan <- stripe
		stripeNum++

		// Stop if we hit EOF on second chunk
		if err2 == io.EOF {
			break
		}
	}
}
