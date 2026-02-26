package dfsclient

import (
	"context"
	"dfs-project/dfspb"
	"fmt"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// DownloadStripeInfo maps stripe metadata
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

// DownloadedChunk represents result of downloading a single chunk
type DownloadedChunk struct {
	ChunkID  string
	Data     []byte
	Success  bool
	Error    error
	IsData1  bool
	IsData2  bool
	IsParity bool
}

// StripeDownload holds downloaded chunks
type StripeDownload struct {
	StripeNum   int
	DataChunk1  []byte
	DataChunk2  []byte
	ParityChunk []byte
	ChunksOK    int
}

// downloadChunkFromServer downloads a single chunk
func (g *GrpcClient) downloadChunkFromServer(chunkID, serverAddr string, username string, isData1, isData2, isParity bool) DownloadedChunk {
	res := DownloadedChunk{ChunkID: chunkID, IsData1: isData1, IsData2: isData2, IsParity: isParity}
	// Use project convention: grpc.NewClient to create connection
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		res.Success = false
		res.Error = fmt.Errorf("connection failed: %v", err)
		return res
	}
	defer conn.Close()

	cli := dfspb.NewChunkServerClient(conn)
	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), 8*time.Second)
	resp, err := cli.ReadChunk(rpcCtx, &dfspb.ReadChunkRequest{ChunkId: chunkID, Username: username})
	rpcCancel()
	if err != nil {
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

// downloadStripe downloads 3 chunks in parallel
func (g *GrpcClient) downloadStripe(info DownloadStripeInfo, username string) StripeDownload {
	var wg sync.WaitGroup
	ch := make(chan DownloadedChunk, 3)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if info.DataChunk1.ChunkID == "" {
			ch <- DownloadedChunk{Success: true, IsData1: true}
			return
		}
		ch <- chunkDownloader(g, info.DataChunk1.ChunkID, info.DataChunk1.Server, username, true, false, false)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		if info.DataChunk2.ChunkID == "" {
			ch <- DownloadedChunk{Success: true, IsData2: true}
			return
		}
		ch <- chunkDownloader(g, info.DataChunk2.ChunkID, info.DataChunk2.Server, username, false, true, false)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		if info.ParityChunk.ChunkID == "" {
			ch <- DownloadedChunk{Success: true, IsParity: true}
			return
		}
		ch <- chunkDownloader(g, info.ParityChunk.ChunkID, info.ParityChunk.Server, username, false, false, true)
	}()

	wg.Wait()
	close(ch)

	sd := StripeDownload{StripeNum: info.StripeNum}
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
		}
	}
	return sd
}

// reconstructMissingChunk uses XOR to rebuild missing data chunk
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

// writeStripeToFile writes chunk data, trimming padding on last stripe
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
