// package dfsclient

// import (
// 	"context"
// 	"dfs-project/pkg/config"
// 	"fmt"
// 	"sync"
// )

// // uploadStripesStreaming uploads stripes read from stripeChan using parallel chunk uploads
// func (g *GrpcClient) uploadStripesStreaming(stripeChan <-chan Stripe, ack *AckQueue, clientID int64, username string, filename string) ([]string, error) {
// 	var successful []string
// 	logger := GetUserLogger()

// 	// Log upload start if username provided
// 	if username != "" {
// 		_ = logger.LogChunkUploadStart(username, filename)
// 	}

// 	stripeCount := 0
// 	for stripe := range stripeChan {
// 		stripeCount = stripe.StripeNum + 1 // track max stripe number

// 		// add all chunk IDs to ack queue
// 		for _, cid := range stripe.ChunkIDs {
// 			if cid == "" {
// 				continue
// 			}
// 			ack.Add(cid)
// 		}

// 		// spawn upload goroutines for each chunk
// 		type res struct {
// 			chunkID string
// 			err     error
// 			index   int
// 		}

// 		resCh := make(chan res, 3)
// 		var wg sync.WaitGroup

// 		for i := 0; i < 3; i++ {
// 			cid := stripe.ChunkIDs[i]
// 			if cid == "" {
// 				continue
// 			}
// 			server := stripe.Servers[i]
// 			payload := []byte{}
// 			switch i {
// 			case 0:
// 				payload = stripe.DataChunk1
// 			case 1:
// 				payload = stripe.DataChunk2
// 			case 2:
// 				payload = stripe.ParityChunk
// 			}

// 			wg.Add(1)
// 			go func(server, cid string, payload []byte, chunkIndex int) {
// 				defer wg.Done()
// 				cctx, cancel := context.WithTimeout(context.Background(), config.ChunkWriteTimeout)
// 				defer cancel()
// 				if err := chunkUploader(cctx, server, cid, payload, clientID, username); err != nil {
// 					resCh <- res{chunkID: cid, err: err, index: chunkIndex}
// 					return
// 				}
// 				// remove from ack queue on success
// 				ack.Remove(cid)
// 				resCh <- res{chunkID: cid, err: nil, index: chunkIndex}
// 			}(server, cid, payload, i)
// 		}

// 		wg.Wait()
// 		close(resCh)

// 		for r := range resCh {
// 			if r.err != nil {
// 				// Log chunk upload failure
// 				if username != "" {
// 					_ = logger.LogChunkUploadFailed(username, filename, r.chunkID, stripe.StripeNum, r.err.Error())
// 				}
// 				return successful, fmt.Errorf("upload failed for chunk %s: %w", r.chunkID, r.err)
// 			}
// 			// Log chunk upload success
// 			if username != "" {
// 				chunkSize := int64(len(stripe.DataChunk1))
// 				if r.index == 1 {
// 					chunkSize = int64(len(stripe.DataChunk2))
// 				} else if r.index == 2 {
// 					chunkSize = int64(len(stripe.ParityChunk))
// 				}
// 				_ = logger.LogChunkUploaded(username, filename, r.chunkID, chunkSize, stripe.StripeNum)
// 			}
// 			successful = append(successful, r.chunkID)
// 		}
// 	}

// 	// Log upload complete
// 	if username != "" && stripeCount > 0 {
// 		_ = logger.LogFileUploadComplete(username, filename, stripeCount)
// 	}

// 	return successful, nil
// }

package dfsclient

import (
	"context"
	"dfs-project/pkg/config"
	"fmt"
	"sync"
)

// UploadStreamStats holds counts collected during uploadStripesStreaming
type UploadStreamStats struct {
	StripeCount     int
	ChunksAttempted int
	ChunksSucceeded int
}

// uploadStripesStreaming uploads stripes and returns chunk IDs, stats, and any error
func (g *GrpcClient) uploadStripesStreaming(stripeChan <-chan Stripe, ack *AckQueue, clientID int64, username string, filename string) ([]string, UploadStreamStats, error) {
	var successful []string
	var stats UploadStreamStats
	logger := GetUserLogger()

	if username != "" {
		_ = logger.LogChunkUploadStart(username, filename)
	}

	for stripe := range stripeChan {
		stats.StripeCount = stripe.StripeNum + 1

		for _, cid := range stripe.ChunkIDs {
			if cid == "" {
				continue
			}
			ack.Add(cid)
		}

		type res struct {
			chunkID string
			err     error
			index   int
		}

		resCh := make(chan res, 3)
		var wg sync.WaitGroup

		for i := 0; i < 3; i++ {
			cid := stripe.ChunkIDs[i]
			if cid == "" {
				continue
			}
			server := stripe.Servers[i]
			payload := []byte{}
			switch i {
			case 0:
				payload = stripe.DataChunk1
			case 1:
				payload = stripe.DataChunk2
			case 2:
				payload = stripe.ParityChunk
			}

			stats.ChunksAttempted++
			wg.Add(1)
			go func(server, cid string, payload []byte, chunkIndex int) {
				defer wg.Done()
				cctx, cancel := context.WithTimeout(context.Background(), config.ChunkWriteTimeout)
				defer cancel()
				if err := chunkUploader(cctx, server, cid, payload, clientID, username); err != nil {
					resCh <- res{chunkID: cid, err: err, index: chunkIndex}
					return
				}
				ack.Remove(cid)
				resCh <- res{chunkID: cid, err: nil, index: chunkIndex}
			}(server, cid, payload, i)
		}

		wg.Wait()
		close(resCh)

		for r := range resCh {
			if r.err != nil {
				if username != "" {
					_ = logger.LogChunkUploadFailed(username, filename, r.chunkID, stripe.StripeNum, r.err.Error())
				}
				return successful, stats, fmt.Errorf("upload failed for chunk %s: %w", r.chunkID, r.err)
			}
			if username != "" {
				chunkSize := int64(len(stripe.DataChunk1))
				if r.index == 1 {
					chunkSize = int64(len(stripe.DataChunk2))
				} else if r.index == 2 {
					chunkSize = int64(len(stripe.ParityChunk))
				}
				_ = logger.LogChunkUploaded(username, filename, r.chunkID, chunkSize, stripe.StripeNum)
			}
			successful = append(successful, r.chunkID)
			stats.ChunksSucceeded++
		}
	}

	if username != "" && stats.StripeCount > 0 {
		_ = logger.LogFileUploadComplete(username, filename, stats.StripeCount)
	}

	return successful, stats, nil
}
