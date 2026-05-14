// Provide a reusable go client API for HIGH-LEVEL DFS operations so the http
// server can call DFS functions without dealing with low-level gRPC details.
package dfsclient

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"dfs-project/dfspb"
	"dfs-project/pkg/config"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// CHUNK_SIZE is an alias for config.ChunkSize kept for internal readability.
// To change the chunk size, edit pkg/config/config.go — do NOT touch this line.
const CHUNK_SIZE = config.ChunkSize

// Interface exposes high-level DFS operations used by HTTP handlers
type Client interface {
	ListFiles(ctx context.Context, clientID int64) ([]string, error)
	DeleteFile(ctx context.Context, clientID int64, filename string, username string) (int, error)
	UploadFile(ctx context.Context, clientID int64, filename string, data io.ReadSeeker, size int64, username string) (int64, TransferStats, error)
	DownloadFile(ctx context.Context, clientID int64, filename string, destPath string, username string) (TransferStats, error)
}

// GrpcClient implements Client using the existing gRPC Master/ChunkServer APIs
type GrpcClient struct {
	masterAddr string
	masterConn *grpc.ClientConn
	masterCli  dfspb.MasterServerClient
}

// NewGrpcClient connects to the configured master address and returns a client
func NewGrpcClient(masterAddr string) (*GrpcClient, error) {
	if masterAddr == "" {
		masterAddr = config.GetMasterAddr()
	}
	// Use Dial with ConnectParams to control the minimum connect timeout
	conn, err := grpc.Dial(masterAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithConnectParams(grpc.ConnectParams{MinConnectTimeout: 5 * time.Second}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to master %s: %w", masterAddr, err)
	}
	cli := dfspb.NewMasterServerClient(conn)
	return &GrpcClient{masterAddr: masterAddr, masterConn: conn, masterCli: cli}, nil
}

func (g *GrpcClient) Close() error {
	if g.masterConn != nil {
		return g.masterConn.Close()
	}
	return nil
}

func (g *GrpcClient) ListFiles(ctx context.Context, clientID int64) ([]string, error) {
	resp, err := g.masterCli.ListFiles(ctx, &dfspb.ListFilesRequest{ClientId: clientID})
	if err != nil {
		return nil, err
	}
	return resp.Filenames, nil
}

func (g *GrpcClient) DeleteFile(ctx context.Context, clientID int64, filename string, username string) (int, error) {
	resp, err := g.masterCli.DeleteFile(ctx, &dfspb.DeleteFileRequest{ClientId: clientID, Filename: filename})
	if err != nil {
		return 0, err
	}
	if !resp.Success {
		return 0, fmt.Errorf("delete failed: %s", resp.Message)
	}
	// Parse deleted chunks count from message if present
	var deleted int
	_, err = fmt.Sscanf(resp.Message, "deleted %d chunks", &deleted)
	if err != nil {
		deleted = 0
	}

	if username != "" {
		logger := GetUserLogger()
		_ = logger.LogFileDeleted(username, filename, deleted)
	}

	return deleted, nil
}

// UploadFile uploads content from reader to the DFS and returns the assigned clientID plus TransferStats
func (g *GrpcClient) UploadFile(ctx context.Context, clientID int64, filename string, data io.ReadSeeker, size int64, username string) (int64, TransferStats, error) {
	var stats TransferStats

	// ── Master RPC: CreateFile ────────────────────────────────────────────────
	t := newPhaseTimer()
	createReq := &dfspb.CreateFileRequest{Filename: filename, TotalSize: size, ClientId: clientID, Username: username}
	createResp, err := g.masterCli.CreateFile(ctx, createReq)
	stats.MasterRPCMs += t.ElapsedMs()
	if err != nil {
		return 0, stats, fmt.Errorf("CreateFile failed: %w", err)
	}
	assignedClient := createResp.ClientId

	cleanupOnFailure := func(cause error) error {
		rctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, derr := g.masterCli.DeleteFile(rctx, &dfspb.DeleteFileRequest{ClientId: assignedClient, Filename: filename}); derr != nil {
			return fmt.Errorf("%w (rollback failed: %v)", cause, derr)
		}
		return cause
	}

	stripesMap := createResp.Stripes

	// ── Parity compute + data transfer ───────────────────────────────────────
	stripeChan := make(chan Stripe, 2)
	errChan := make(chan error, 1)

	parityStart := newPhaseTimer()
	go g.streamFileInStripes(data, stripesMap, stripeChan, errChan)

	ack := NewAckQueue()
	transferStart := newPhaseTimer()
	successfulChunks, uploadStats, err := g.uploadStripesStreaming(stripeChan, ack, assignedClient, username, filename)
	stats.DataTransferMs = transferStart.ElapsedMs()
	stats.ParityComputeMs = parityStart.ElapsedMs() - stats.DataTransferMs
	if stats.ParityComputeMs < 0 {
		stats.ParityComputeMs = 0
	}
	stats.StripeCount = uploadStats.StripeCount
	stats.ChunksAttempted = uploadStats.ChunksAttempted
	stats.ChunksSucceeded = uploadStats.ChunksSucceeded
	if err != nil {
		return assignedClient, stats, cleanupOnFailure(err)
	}

	select {
	case err := <-errChan:
		if err != nil {
			return assignedClient, stats, cleanupOnFailure(err)
		}
	default:
	}

	// ── Master RPC: ConfirmWrite ──────────────────────────────────────────────
	if len(successfulChunks) > 0 {
		t2 := newPhaseTimer()
		_, err := g.masterCli.ConfirmWrite(ctx, &dfspb.ConfirmWriteRequest{Filename: filename, ChunkIds: successfulChunks})
		stats.MasterRPCMs += t2.ElapsedMs()
		if err != nil {
			return assignedClient, stats, cleanupOnFailure(fmt.Errorf("confirm write failed: %w", err))
		}
	}

	return assignedClient, stats, nil
}

func writeChunkToServer(ctx context.Context, serverAddr string, chunkID string, data []byte, clientID int64, username string) error {
	if serverAddr == "" {
		return fmt.Errorf("empty server address")
	}

	checksum := calculateChecksum(data)
	var lastErr error

	for attempt := 0; attempt <= maxChunkRetries; attempt++ {
		conn, err := defaultPool.Get(serverAddr)
		if err != nil {
			lastErr = err
			defaultPool.Evict(serverAddr)
			continue
		}

		chunkCli := dfspb.NewChunkServerClient(conn)
		_, err = chunkCli.WriteChunk(ctx, &dfspb.WriteChunkRequest{
			ChunkId:  chunkID,
			Data:     data,
			Checksum: checksum,
			ClientId: clientID,
			Username: username,
		})
		if err == nil {
			return nil
		}

		lastErr = err
		if isTransientErr(err) {
			defaultPool.Evict(serverAddr)
			continue
		}
		// Non-transient error (e.g. invalid chunk ID) — don't retry
		return err
	}

	return fmt.Errorf("WriteChunk to %s failed after %d attempts: %w", serverAddr, maxChunkRetries+1, lastErr)
}

// chunkUploader is a test hook; by default points to real writeChunkToServer implementation
var chunkUploader = writeChunkToServer

// chunkDownloader is a test hook for download; by default calls the real method
var chunkDownloader = func(g *GrpcClient, chunkID, serverAddr string, clientID int64, username string, isData1, isData2, isParity bool) DownloadedChunk {
	return g.downloadChunkFromServer(chunkID, serverAddr, clientID, username, isData1, isData2, isParity)
}

// DownloadFile downloads a file and writes to destPath, returning TransferStats
func (g *GrpcClient) DownloadFile(ctx context.Context, clientID int64, filename string, destPath string, username string) (TransferStats, error) {
	var stats TransferStats
	logger := GetUserLogger()

	if username != "" {
		_ = logger.LogDownloadStart(username, filename)
	}

	// ── Master RPC: GetFileMetadata ───────────────────────────────────────────
	t := newPhaseTimer()
	meta, err := g.masterCli.GetFileMetadata(ctx, &dfspb.GetFileMetadataRequest{ClientId: clientID, Filename: filename})
	stats.MasterRPCMs += t.ElapsedMs()
	if err != nil {
		return stats, fmt.Errorf("GetFileMetadata failed: %w", err)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return stats, fmt.Errorf("failed to create dest file: %w", err)
	}
	defer f.Close()

	keys := make([]int, 0, len(meta.Stripes))
	for k := range meta.Stripes {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)
	stats.StripeCount = len(keys)

	bytesWritten := int64(0)
	for idx, k := range keys {
		s := meta.Stripes[int32(k)]

		info := DownloadStripeInfo{
			StripeNum:   k,
			DataChunk1:  ChunkServerPair{ChunkID: s.ChunkIds[0], Server: s.Servers[0]},
			DataChunk2:  ChunkServerPair{ChunkID: s.ChunkIds[1], Server: s.Servers[1]},
			ParityChunk: ChunkServerPair{ChunkID: s.ChunkIds[2], Server: s.Servers[2]},
		}

		// ── Data transfer: download stripe chunks ─────────────────────────────
		tXfer := newPhaseTimer()
		sd := g.downloadStripe(info, clientID, username, filename)
		stats.DataTransferMs += tXfer.ElapsedMs()

		stats.ChunksAttempted += 3
		stats.ChunksSucceeded += sd.ChunksOK

		if sd.ChunksOK < 2 {
			return stats, fmt.Errorf("insufficient chunks available to reconstruct stripe %d", k)
		}
		if sd.ChunksOK < 3 {
			// ── Reconstruction ───────────────────────────────────────────────
			tRecon := newPhaseTimer()
			if err := reconstructMissingChunk(&sd); err != nil {
				return stats, fmt.Errorf("reconstruction failed for stripe %d: %w", k, err)
			}
			stats.ReconstructionMs += tRecon.ElapsedMs()
			stats.ChunksReconstructed++
		}

		isLast := idx == len(keys)-1
		n, err := writeStripeToFile(f, &sd, isLast, meta.FileSize, bytesWritten)
		if err != nil {
			return stats, err
		}
		bytesWritten += int64(n)
	}

	if username != "" {
		_ = logger.LogFileDownloadComplete(username, filename, len(keys))
	}

	return stats, nil
}
