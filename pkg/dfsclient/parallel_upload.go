package dfsclient

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// uploadStripesStreaming uploads stripes read from stripeChan using parallel chunk uploads
func (g *GrpcClient) uploadStripesStreaming(stripeChan <-chan Stripe, ack *AckQueue, clientID int64) ([]string, error) {
	var successful []string

	for stripe := range stripeChan {
		// add all chunk IDs to ack queue
		for _, cid := range stripe.ChunkIDs {
			if cid == "" {
				continue
			}
			ack.Add(cid)
		}

		// spawn upload goroutines for each chunk
		type res struct{
			chunkID string
			err error
		}

		resCh := make(chan res, 3)
		var wg sync.WaitGroup

		for i := 0; i < 3; i++ {
			cid := stripe.ChunkIDs[i]
			if cid == "" { continue }
			server := stripe.Servers[i]
			payload := []byte{}
			switch i {
			case 0: payload = stripe.DataChunk1
			case 1: payload = stripe.DataChunk2
			case 2: payload = stripe.ParityChunk
			}

			wg.Add(1)
			go func(server, cid string, payload []byte) {
				defer wg.Done()
				cctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
				defer cancel()
				if err := chunkUploader(cctx, server, cid, payload, clientID); err != nil {
					resCh <- res{chunkID: cid, err: err}
					return
				}
				// remove from ack queue on success
				ack.Remove(cid)
				resCh <- res{chunkID: cid, err: nil}
			}(server, cid, payload)
		}

		wg.Wait()
		close(resCh)

		for r := range resCh {
			if r.err != nil {
				return successful, fmt.Errorf("upload failed for chunk %s: %w", r.chunkID, r.err)
			}
			successful = append(successful, r.chunkID)
		}
	}

	return successful, nil
}
