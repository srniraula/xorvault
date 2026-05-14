package dfsclient

import (
	"context"
	"dfs-project/dfspb"
	"dfs-project/pkg/config"
	"fmt"
	"os"
	"sync"
)

type DownloadStripeInfo struct {
	StripeNum   int
	DataChunk1  ChunkServerPair
	DataChunk2  ChunkServerPair
	ParityChunk ChunkServerPair
}

type ChunkServerPair struct {
	ChunkID string
	Server  string
}

// DownloadedChunk represents the result of downloading a single chunk
type DownloadedChunk struct {
	ChunkID   string
	Data      []byte
	Success   bool
	Error     error
	IsData1   bool
	IsData2   bool
	IsParity  bool
	Filename  string
	StripeNum int
}

// StripeDownload holds the actual data retrieved for a stripe
type StripeDownload struct {
	StripeNum   int
	DataChunk1  []byte
	DataChunk2  []byte
	ParityChunk []byte
	ChunksOK    int
}

// downloadChunkFromServer downloads a single chunk using the connection pool
// with retry on transient errors.
func (g *GrpcClient) downloadChunkFromServer(chunkID, serverAddr string, clientID int64, username string, isData1, isData2, isParity bool) DownloadedChunk {
	res := DownloadedChunk{ChunkID: chunkID, IsData1: isData1, IsData2: isData2, IsParity: isParity}

	var lastErr error
	for attempt := 0; attempt <= maxChunkRetries; attempt++ {
		conn, err := defaultPool.Get(serverAddr)
		if err != nil {
			lastErr = err
			defaultPool.Evict(serverAddr)
			continue
		}

		cli := dfspb.NewChunkServerClient(conn)
		rpcCtx, rpcCancel := context.WithTimeout(context.Background(), config.ChunkReadTimeout)
		resp, err := cli.ReadChunk(rpcCtx, &dfspb.ReadChunkRequest{ChunkId: chunkID, ClientId: clientID, Username: username})
		rpcCancel()

		if err != nil {
			lastErr = err
			if isTransientErr(err) {
				defaultPool.Evict(serverAddr)
				continue
			}
			// Non-transient error — don't retry
			res.Success = false
			res.Error = fmt.Errorf("ReadChunk RPC failed: %v", err)
			return res
		}

		if resp.Checksum != "" {
			if calculateChecksum(resp.Data) != resp.Checksum {
				res.Success = false
				res.Error = fmt.Errorf("checksum mismatch: expected %s got %s", resp.Checksum, calculateChecksum(resp.Data))
				return res
			}
		}

		res.Data = resp.Data
		res.Success = true
		return res
	}

	res.Success = false
	res.Error = fmt.Errorf("ReadChunk from %s failed after %d attempts: %v", serverAddr, maxChunkRetries+1, lastErr)
	return res
}

// downloadStripe downloads 3 chunks in parallel
func (g *GrpcClient) downloadStripe(info DownloadStripeInfo, clientID int64, username string, filename string) StripeDownload {
	var wg sync.WaitGroup
	ch := make(chan DownloadedChunk, 3)

	wg.Add(1)
	go func() {
		defer wg.Done()
		dc := chunkDownloader(g, info.DataChunk1.ChunkID, info.DataChunk1.Server, clientID, username, true, false, false)
		dc.Filename = filename
		dc.StripeNum = info.StripeNum
		ch <- dc
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		dc := chunkDownloader(g, info.DataChunk2.ChunkID, info.DataChunk2.Server, clientID, username, false, true, false)
		dc.Filename = filename
		dc.StripeNum = info.StripeNum
		ch <- dc
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		dc := chunkDownloader(g, info.ParityChunk.ChunkID, info.ParityChunk.Server, clientID, username, false, false, true)
		dc.Filename = filename
		dc.StripeNum = info.StripeNum
		ch <- dc
	}()

	wg.Wait()
	close(ch)

	sd := StripeDownload{StripeNum: info.StripeNum}
	logger := GetUserLogger()
	for r := range ch {
		if r.Success {
			sd.ChunksOK++
			if r.IsData1 {
				sd.DataChunk1 = r.Data
			}
			if r.IsData2 {
				sd.DataChunk2 = r.Data
			}
			if r.IsParity {
				sd.ParityChunk = r.Data
			}
			if username != "" && filename != "" {
				_ = logger.LogChunkDownloaded(username, filename, r.ChunkID, int64(len(r.Data)), r.StripeNum)
			}
		} else {
			if username != "" && filename != "" {
				errMsg := "unknown error"
				if r.Error != nil {
					errMsg = r.Error.Error()
				}
				_ = logger.LogChunkDownloadFailed(username, filename, r.ChunkID, r.StripeNum, errMsg)
			}
		}
	}
	return sd
}

// reconstructMissingChunk uses XOR to rebuild a missing data chunk in RAID-4
func reconstructMissingChunk(stripe *StripeDownload) error {
	if stripe.ChunksOK == 3 {
		return nil
	}
	if stripe.ChunksOK < 2 {
		return fmt.Errorf("insufficient chunks for reconstruction: %d", stripe.ChunksOK)
	}
	if stripe.DataChunk1 == nil && stripe.DataChunk2 != nil && stripe.ParityChunk != nil {
		stripe.DataChunk1 = calculateParity(stripe.DataChunk2, stripe.ParityChunk)
		return nil
	}
	if stripe.DataChunk2 == nil && stripe.DataChunk1 != nil && stripe.ParityChunk != nil {
		stripe.DataChunk2 = calculateParity(stripe.DataChunk1, stripe.ParityChunk)
		return nil
	}
	if stripe.ParityChunk == nil && stripe.DataChunk1 != nil && stripe.DataChunk2 != nil {
		return nil
	}
	return fmt.Errorf("unexpected chunk combination for reconstruction")
}

// writeStripeToFile writes chunk data to disk, trimming parity padding on the last stripe
func writeStripeToFile(file *os.File, stripe *StripeDownload, isLast bool, originalSize int64, bytesWritten int64) (int, error) {
	total := 0
	if stripe.DataChunk1 != nil {
		data := stripe.DataChunk1
		if isLast {
			remaining := originalSize - bytesWritten
			if remaining < int64(len(data)) {
				data = data[:remaining]
			}
		}
		n, err := file.Write(data)
		if err != nil {
			return total, fmt.Errorf("failed write data1: %v", err)
		}
		total += n
		bytesWritten += int64(n)
	}
	if stripe.DataChunk2 != nil {
		data := stripe.DataChunk2
		if isLast {
			remaining := originalSize - bytesWritten
			if remaining < int64(len(data)) {
				data = data[:remaining]
			}
		}
		n, err := file.Write(data)
		if err != nil {
			return total, fmt.Errorf("failed write data2: %v", err)
		}
		total += n
	}
	return total, nil
}
