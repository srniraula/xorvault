package dfsclient

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// uploadStripesStreaming uploads stripes read from stripeChan using parallel chunk uploads
func (g *GrpcClient) uploadStripesStreaming(stripeChan <-chan Stripe, ack *AckQueue, username string) ([]string, error) {
	var successful []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	var errOnce sync.Once
	var firstErr error

	// Use a semaphore to limit concurrent chunk uploads (e.g., 20 at a time)
	sem := make(chan struct{}, 20)

	for stripe := range stripeChan {
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

			// add all chunk IDs to ack queue
			ack.Add(cid)

			wg.Add(1)
			go func(server, cid string, payload []byte) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				cctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()

				if err := chunkUploader(cctx, server, cid, payload, username); err != nil {
					errOnce.Do(func() { firstErr = fmt.Errorf("upload failed for chunk %s: %w", cid, err) })
					return
				}
				// remove from ack queue on success
				ack.Remove(cid)
				
				mu.Lock()
				successful = append(successful, cid)
				mu.Unlock()
			}(server, cid, payload)
		}
	}

	wg.Wait()

	if firstErr != nil {
		return successful, firstErr
	}

	return successful, nil
}
