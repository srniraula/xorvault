// package main

// import (
// 	"context"
// 	"dfs-project/dfspb"
// 	"fmt"
// 	"log"
// 	"sync"

// 	"google.golang.org/grpc"
// 	"google.golang.org/grpc/credentials/insecure"
// )

// // UploadTask represents a single chunk upload task
// type UploadTask struct {
// 	ServerAddr string
// 	ChunkID    string
// 	Data       []byte
// 	Checksum   string
// 	ClientID   int64
// }

// // UploadResult contains the result of a chunk upload
// type UploadResult struct {
// 	ChunkID  string
// 	Success  bool
// 	Error    error
// 	Checksum string
// }

// // uploadStripesStreaming consumes stripes from a channel and uploads chunks in parallel
// // Uses pipeline pattern - uploads chunks as stripes arrive, no memory accumulation
// // Returns list of successfully uploaded chunk IDs
// func uploadStripesStreaming(stripeChan <-chan Stripe, ackQueue *AckQueue, clientID int64) ([]string, error) {
// 	var wg sync.WaitGroup

// 	// Result channel - buffered to prevent upload goroutines from blocking
// 	resultChan := make(chan UploadResult, 6)

// 	// Result collector
// 	successfulChunks := make([]string, 0)
// 	var resultMu sync.Mutex
// 	collectorDone := make(chan bool)
// 	uploadedCount := 0

// 	go func() {
// 		for result := range resultChan {
// 			uploadedCount++

// 			if result.Success {
// 				resultMu.Lock()
// 				successfulChunks = append(successfulChunks, result.ChunkID)
// 				resultMu.Unlock()

// 				log.Printf("[%d] Uploaded %s (checksum: %s...)",
// 					uploadedCount, result.ChunkID, result.Checksum[:8])
// 			} else {
// 				log.Printf("[%d] Failed %s: %v",
// 					uploadedCount, result.ChunkID, result.Error)
// 			}
// 		}
// 		collectorDone <- true
// 	}()

// 	// Consume stripes from channel and spawn upload goroutines
// 	// this loop is blocking
// 	stripeCount := 0
// 	for stripe := range stripeChan {
// 		stripeCount++
// 		// Add all chunk IDs to ACK queue for this stripe
// 		for _, chunkID := range stripe.ChunkIDs {
// 			if len(chunkID) == 0 {
// 				continue
// 			}
// 			ackQueue.Add(chunkID)
// 		}

// 		// Upload 3 chunks (2 data + 1 parity) for this stripe
// 		tasks := []UploadTask{
// 			{
// 				ServerAddr: stripe.Servers[0],
// 				ChunkID:    stripe.ChunkIDs[0],
// 				Data:       stripe.DataChunk1,
// 				Checksum:   stripe.Checksums[0],
// 				ClientID:   clientID,
// 			},
// 			{
// 				ServerAddr: stripe.Servers[1],
// 				ChunkID:    stripe.ChunkIDs[1],
// 				Data:       stripe.DataChunk2,
// 				Checksum:   stripe.Checksums[1],
// 				ClientID:   clientID,
// 			},
// 			{
// 				ServerAddr: stripe.Servers[2],
// 				ChunkID:    stripe.ChunkIDs[2],
// 				Data:       stripe.ParityChunk,
// 				Checksum:   stripe.Checksums[2],
// 				ClientID:   clientID,
// 			},
// 		}

// 		// Spawn 3 upload goroutines for this stripe
// 		// for odd number of chunks of a file, we skip uploadchunk
// 		// because chunk is empty
// 		for _, task := range tasks {
// 			if len(task.ChunkID) == 0 {
// 				continue
// 			}
// 			wg.Add(1)
// 			go uploadChunk(task, &wg, resultChan, ackQueue)
// 		}

// 		// Stripe data can now be garbage collected - not held in memory
// 	}

// 	stripeWord := "stripe"
// 	if stripeCount != 1 {
// 		stripeWord = "stripes"
// 	}
// 	log.Printf("Received %d %s from stream, waiting for uploads to complete", stripeCount, stripeWord)

// 	// Wait for all uploads to finish
// 	wg.Wait()
// 	close(resultChan)
// 	<-collectorDone

// 	return successfulChunks, nil
// }

// // chan<- Type    // Send-only (can only WRITE to it)
// // <-chan Type    // Receive-only (can only READ from it)
// // chan Type      // Bidirectional (can both read and write)

// // uploadChunk uploads a single chunk to its designated server
// func uploadChunk(task UploadTask, wg *sync.WaitGroup, resultChan chan<- UploadResult, ackQueue *AckQueue) {
// 	defer wg.Done() // added first, executes last

// 	result := UploadResult{
// 		ChunkID:  task.ChunkID,
// 		Checksum: task.Checksum,
// 	}

// 	// TODO 6.4: Inside goroutine: connect to chunk server
// 	conn, err := grpc.NewClient(task.ServerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
// 	if err != nil {
// 		result.Success = false
// 		result.Error = fmt.Errorf("connection failed: %v", err)
// 		// TODO 6.7: On failure: log error but don't remove from ACK queue
// 		resultChan <- result
// 		return
// 	}
// 	defer conn.Close()
// 	client := dfspb.NewChunkServerClient(conn)

// 	// TODO 6.5: Inside goroutine: send chunk via WriteChunk RPC
// 	_, err = client.WriteChunk(context.Background(), &dfspb.WriteChunkRequest{
// 		ChunkId:  task.ChunkID,
// 		Data:     task.Data,
// 		Checksum: task.Checksum,
// 		ClientId: task.ClientID,
// 	})

// 	if err != nil {
// 		result.Success = false
// 		result.Error = fmt.Errorf("WriteChunk RPC failed: %v", err)
// 		// TODO 6.7: On failure: don't remove from ACK queue
// 		resultChan <- result
// 		return
// 	}

// 	// TODO 6.6: On success: remove chunk from ACK queue
// 	ackQueue.Remove(task.ChunkID)
// 	result.Success = true

// 	// TODO 6.8: Send result to result channel
// 	resultChan <- result
// }

package main

import (
	"context"
	"dfs-project/dfspb"
	"fmt"
	"log"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// UploadTask represents a single chunk upload task
type UploadTask struct {
	ServerAddr string
	ChunkID    string
	Data       []byte
	Checksum   string
	ClientID   int64
}

// UploadResult contains the result of a chunk upload
type UploadResult struct {
	ChunkID  string
	Success  bool
	Error    error
	Checksum string
}

// uploadStripesStreaming consumes stripes from a channel and uploads chunks in parallel
// Uses pipeline pattern - uploads chunks as stripes arrive, no memory accumulation
// Returns list of successfully uploaded chunk IDs
func uploadStripesStreaming(stripeChan <-chan Stripe, ackQueue *AckQueue, clientID int64) ([]string, error) {
	var wg sync.WaitGroup

	// Result channel - buffered to prevent upload goroutines from blocking
	resultChan := make(chan UploadResult, 6)

	// Result collector
	successfulChunks := make([]string, 0)
	var resultMu sync.Mutex
	collectorDone := make(chan bool)
	uploadedCount := 0

	go func() {
		for result := range resultChan {
			uploadedCount++

			if result.Success {
				resultMu.Lock()
				successfulChunks = append(successfulChunks, result.ChunkID)
				resultMu.Unlock()

				log.Printf("[%d] Uploaded %s (checksum: %s...)",
					uploadedCount, result.ChunkID, result.Checksum[:8])
			} else {
				log.Printf("[%d] Failed %s: %v",
					uploadedCount, result.ChunkID, result.Error)
			}
		}
		collectorDone <- true
	}()

	// Consume stripes from channel and spawn upload goroutines
	// this loop is blocking
	stripeCount := 0
	for stripe := range stripeChan {
		stripeCount++
		// Add all chunk IDs to ACK queue for this stripe
		for _, chunkID := range stripe.ChunkIDs {
			if len(chunkID) == 0 {
				continue
			}
			ackQueue.Add(chunkID)
		}

		// Upload 3 chunks (2 data + 1 parity) for this stripe
		tasks := []UploadTask{
			{
				ServerAddr: stripe.Servers[0],
				ChunkID:    stripe.ChunkIDs[0],
				Data:       stripe.DataChunk1,
				Checksum:   stripe.Checksums[0],
				ClientID:   clientID,
			},
			{
				ServerAddr: stripe.Servers[1],
				ChunkID:    stripe.ChunkIDs[1],
				Data:       stripe.DataChunk2,
				Checksum:   stripe.Checksums[1],
				ClientID:   clientID,
			},
			{
				ServerAddr: stripe.Servers[2],
				ChunkID:    stripe.ChunkIDs[2],
				Data:       stripe.ParityChunk,
				Checksum:   stripe.Checksums[2],
				ClientID:   clientID,
			},
		}

		// Spawn 3 upload goroutines for this stripe
		// for odd number of chunks of a file, we skip uploadchunk
		// because chunk is empty
		for _, task := range tasks {
			if len(task.ChunkID) == 0 {
				continue
			}
			if len(task.ServerAddr) == 0 {
				// Master returned no server for this chunk — not enough chunk servers
				// have re-registered yet after a failover.  Log and skip; the upload
				// will be reported incomplete so the caller can retry.
				log.Printf("Warning: no server assigned for chunk %s (chunk servers still re-registering after failover?)", task.ChunkID)
				ackQueue.Remove(task.ChunkID) // don't leave it pending forever
				continue
			}
			wg.Add(1)
			go uploadChunk(task, &wg, resultChan, ackQueue)
		}

		// Stripe data can now be garbage collected - not held in memory
	}

	stripeWord := "stripe"
	if stripeCount != 1 {
		stripeWord = "stripes"
	}
	log.Printf("Received %d %s from stream, waiting for uploads to complete", stripeCount, stripeWord)

	// Wait for all uploads to finish
	wg.Wait()
	close(resultChan)
	<-collectorDone

	return successfulChunks, nil
}

// chan<- Type    // Send-only (can only WRITE to it)
// <-chan Type    // Receive-only (can only READ from it)
// chan Type      // Bidirectional (can both read and write)

// uploadChunk uploads a single chunk to its designated server
func uploadChunk(task UploadTask, wg *sync.WaitGroup, resultChan chan<- UploadResult, ackQueue *AckQueue) {
	defer wg.Done() // added first, executes last

	result := UploadResult{
		ChunkID:  task.ChunkID,
		Checksum: task.Checksum,
	}

	// TODO 6.4: Inside goroutine: connect to chunk server
	conn, err := grpc.NewClient(task.ServerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		result.Success = false
		result.Error = fmt.Errorf("connection failed: %v", err)
		// TODO 6.7: On failure: log error but don't remove from ACK queue
		resultChan <- result
		return
	}
	defer conn.Close()
	client := dfspb.NewChunkServerClient(conn)

	// TODO 6.5: Inside goroutine: send chunk via WriteChunk RPC
	_, err = client.WriteChunk(context.Background(), &dfspb.WriteChunkRequest{
		ChunkId:  task.ChunkID,
		Data:     task.Data,
		Checksum: task.Checksum,
		ClientId: task.ClientID,
	})

	if err != nil {
		result.Success = false
		result.Error = fmt.Errorf("WriteChunk RPC failed: %v", err)
		// TODO 6.7: On failure: don't remove from ACK queue
		resultChan <- result
		return
	}

	// TODO 6.6: On success: remove chunk from ACK queue
	ackQueue.Remove(task.ChunkID)
	result.Success = true

	// TODO 6.8: Send result to result channel
	resultChan <- result
}
