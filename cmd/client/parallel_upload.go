package main

import (
	"context"
	"dfs-project/dfspb"
	"log"
	"sync"
	"time"

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

// Concurrency limits — tune these to match your network:
//
//	concurrencyLocal  : goroutines per chunkserver when it's on the same machine
//	                    or a virtual interface (UTM/VM).
//	concurrencyWiFi   : goroutines per chunkserver over WiFi or slow LAN.
//	                    Formula: WiFi_MB/s / 1MB_chunk_size = safe concurrent count
const (
	concurrencyLocal = 8 // mac↔VM: virtual network, max speed
	concurrencyWiFi  = 2 // mac↔Lenovo: real WiFi, conservative
)

// localPrefixes contains network prefixes that are considered "local" —
// same machine or virtual interfaces.
var localPrefixes = []string{
	"127.",         // loopback
	"192.168.128.", // UTM host-only (Mac↔VM)
	"::1",          // IPv6 loopback
}

// concurrencyForAddr returns the concurrency limit for a given server address.
// Local/virtual interfaces get high concurrency; WiFi remotes get low concurrency.
func concurrencyForAddr(addr string) int {
	host := addr
	if i := len(addr) - 1; i >= 0 {
		for i >= 0 && addr[i] != ':' {
			i--
		}
		if i > 0 {
			host = addr[:i]
		}
	}
	for _, prefix := range localPrefixes {
		if len(host) >= len(prefix) && host[:len(prefix)] == prefix {
			return concurrencyLocal
		}
	}
	return concurrencyWiFi
}

// serverSemaphores holds one buffered channel (semaphore) per chunkserver address.
var (
	semMu  sync.Mutex
	semMap = make(map[string]chan struct{})
)

func getServerSem(addr string) chan struct{} {
	semMu.Lock()
	defer semMu.Unlock()
	if _, ok := semMap[addr]; !ok {
		limit := concurrencyForAddr(addr)
		semMap[addr] = make(chan struct{}, limit)
	}
	return semMap[addr]
}

// uploadStripesStreaming consumes stripes from a channel and uploads chunks in parallel.
// Uses pipeline pattern - uploads chunks as stripes arrive, no memory accumulation.
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

	stripeCount := 0
	for stripe := range stripeChan {
		stripeCount++

		for _, chunkID := range stripe.ChunkIDs {
			if len(chunkID) == 0 {
				continue
			}
			ackQueue.Add(chunkID)
		}

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

		for _, task := range tasks {
			if len(task.ChunkID) == 0 {
				continue
			}
			if len(task.ServerAddr) == 0 {
				// Master returned no server for this chunk — not enough chunk servers
				// have re-registered yet after a failover.
				log.Printf("Warning: no server assigned for chunk %s (chunk servers still re-registering after failover?)", task.ChunkID)
				ackQueue.Remove(task.ChunkID)
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

	wg.Wait()
	close(resultChan)
	<-collectorDone

	return successfulChunks, nil
}

// uploadChunk uploads a single chunk to its designated server.
// Acquires a per-server semaphore to limit concurrent uploads to any one
// server (prevents overwhelming a remote WiFi chunkserver).
// Retries up to maxAttempts times with exponential backoff.
func uploadChunk(task UploadTask, wg *sync.WaitGroup, resultChan chan<- UploadResult, ackQueue *AckQueue) {
	defer wg.Done()

	// Acquire per-server slot — blocks if maxConcurrentPerServer already in flight
	sem := getServerSem(task.ServerAddr)
	sem <- struct{}{}
	defer func() { <-sem }()

	result := UploadResult{
		ChunkID:  task.ChunkID,
		Checksum: task.Checksum,
	}

	const (
		initialBackoff = 500 * time.Millisecond
		maxBackoff     = 10 * time.Second
		maxAttempts    = 3 // worst case: ~93s total (3×30s RPC + 0.5+1s sleep)
		rpcTimeout     = 30 * time.Second
	)

	backoff := initialBackoff
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		conn, err := grpc.NewClient(task.ServerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			lastErr = err
			log.Printf("[retry %d/%d] connection to %s failed: %v — retrying in %v",
				attempt, maxAttempts, task.ServerAddr, err, backoff)
			time.Sleep(backoff)
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		client := dfspb.NewChunkServerClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		_, err = client.WriteChunk(ctx, &dfspb.WriteChunkRequest{
			ChunkId:  task.ChunkID,
			Data:     task.Data,
			Checksum: task.Checksum,
			ClientId: task.ClientID,
		})
		cancel()
		conn.Close()

		if err == nil {
			// Success — remove from ACK queue and report
			ackQueue.Remove(task.ChunkID)
			result.Success = true
			resultChan <- result
			return
		}

		lastErr = err
		log.Printf("[retry %d/%d] WriteChunk %s → %s failed: %v — retrying in %v",
			attempt, maxAttempts, task.ChunkID, task.ServerAddr, err, backoff)
		time.Sleep(backoff)
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}

	// All attempts exhausted — report failure, chunk stays in ACK queue
	log.Printf("[FAILED] chunk %s → %s gave up after %d attempts. Last error: %v",
		task.ChunkID, task.ServerAddr, maxAttempts, lastErr)
	result.Success = false
	result.Error = lastErr
	resultChan <- result
}
