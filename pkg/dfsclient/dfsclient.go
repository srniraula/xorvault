// Provide a reusable go client API for HIGL-LEVEL DFS operations so the http
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

const CHUNK_SIZE = 1 * 1024 * 1024

// Interface exposes high-level DFS operations used by HTTP handlers
type Client interface {
	ListFiles(ctx context.Context, clientID int64) ([]string, error)
	DeleteFile(ctx context.Context, clientID int64, filename string, username string) (int, error)
	UploadFile(ctx context.Context, clientID int64, filename string, data io.Reader, size int64, username string) (int64, error)
	DownloadFile(ctx context.Context, clientID int64, filename string, destPath string, username string) error
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
		// couldn't parse, but deletion succeeded
		deleted = 0
	}

	// Log the deletion
	if username != "" {
		logger := GetUserLogger()
		_ = logger.LogFileDeleted(username, filename, deleted)
	}

	return deleted, nil
}

// UploadFile uploads content from reader to the DFS and returns the assigned clientID
func (g *GrpcClient) UploadFile(ctx context.Context, clientID int64, filename string, data io.Reader, size int64, username string) (int64, error) {
	// Create file (master may assign clientID if 0)
	createReq := &dfspb.CreateFileRequest{Filename: filename, TotalSize: size, ClientId: clientID, Username: username}
	createResp, err := g.masterCli.CreateFile(ctx, createReq)
	if err != nil {
		return 0, fmt.Errorf("CreateFile failed: %w", err)
	}
	assignedClient := createResp.ClientId
	// Build stripes map
	stripesMap := createResp.Stripes

	// Stream file in stripes and upload using pipeline similar to cmd/client
	stripeChan := make(chan Stripe, 2)
	errChan := make(chan error, 1)

	go g.streamFileInStripes(data, stripesMap, stripeChan, errChan)

	// ACK queue tracks pending chunks
	ack := NewAckQueue()

	// start uploading stripes as they arrive
	successfulChunks, err := g.uploadStripesStreaming(stripeChan, ack, assignedClient, username, filename)
	if err != nil {
		return assignedClient, err
	}

	// Check producer errors
	select {
	case err := <-errChan:
		if err != nil {
			return assignedClient, err
		}
	default:
	}

	// Confirm write to master
	if len(successfulChunks) > 0 {
		_, err := g.masterCli.ConfirmWrite(ctx, &dfspb.ConfirmWriteRequest{Filename: filename, ChunkIds: successfulChunks})
		if err != nil {
			return assignedClient, fmt.Errorf("confirm write failed: %w", err)
		}
	}

	return assignedClient, nil
}

func writeChunkToServer(ctx context.Context, serverAddr string, chunkID string, data []byte, clientID int64, username string) error {
	if serverAddr == "" {
		return fmt.Errorf("empty server address")
	}

	// Use project convention: grpc.NewClient to create a connection
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("grpc new client failed: %w", err)
	}
	defer conn.Close()

	chunkCli := dfspb.NewChunkServerClient(conn)
	checksum := calculateChecksum(data)

	// Use same ctx for RPC; caller should set an appropriate timeout
	_, err = chunkCli.WriteChunk(ctx, &dfspb.WriteChunkRequest{ChunkId: chunkID, Data: data, Checksum: checksum, ClientId: clientID, Username: username})
	if err != nil {
		return err
	}
	return nil
}

// chunkUploader is a test hook; by default points to real writeChunkToServer implementation
var chunkUploader = writeChunkToServer

// chunkDownloader is a test hook for download; by default calls the real method
var chunkDownloader = func(g *GrpcClient, chunkID, serverAddr string, clientID int64, username string, isData1, isData2, isParity bool) DownloadedChunk {
	return g.downloadChunkFromServer(chunkID, serverAddr, clientID, username, isData1, isData2, isParity)
}

// checksum implementation moved to checksum.go

// helper functions and types were moved to dedicated files under pkg/dfsclient

// DownloadFile downloads a file and writes to destPath
func (g *GrpcClient) DownloadFile(ctx context.Context, clientID int64, filename string, destPath string, username string) error {
	logger := GetUserLogger()

	// Log download start
	if username != "" {
		_ = logger.LogDownloadStart(username, filename)
	}

	// get metadata
	meta, err := g.masterCli.GetFileMetadata(ctx, &dfspb.GetFileMetadataRequest{ClientId: clientID, Filename: filename})
	if err != nil {
		return fmt.Errorf("GetFileMetadata failed: %w", err)
	}

	// open dest file
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create dest file: %w", err)
	}
	defer f.Close()

	// iterate stripes in order
	// stripe keys are int32; sort order important
	keys := make([]int, 0, len(meta.Stripes))
	for k := range meta.Stripes {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)

	bytesWritten := int64(0)
	for idx, k := range keys {
		s := meta.Stripes[int32(k)]

		info := DownloadStripeInfo{
			StripeNum:   k,
			DataChunk1:  ChunkServerPair{ChunkID: s.ChunkIds[0], Server: s.Servers[0]},
			DataChunk2:  ChunkServerPair{ChunkID: s.ChunkIds[1], Server: s.Servers[1]},
			ParityChunk: ChunkServerPair{ChunkID: s.ChunkIds[2], Server: s.Servers[2]},
		}

		sd := g.downloadStripe(info, clientID, username, filename)

		// attempt reconstruction if needed
		if sd.ChunksOK < 2 {
			return fmt.Errorf("insufficient chunks available to reconstruct stripe %d", k)
		}
		if sd.ChunksOK < 3 {
			if err := reconstructMissingChunk(&sd); err != nil {
				return fmt.Errorf("reconstruction failed for stripe %d: %w", k, err)
			}
		}

		isLast := idx == len(keys)-1
		n, err := writeStripeToFile(f, &sd, isLast, meta.FileSize, bytesWritten)
		if err != nil {
			return err
		}
		bytesWritten += int64(n)
	}

	// Log download complete
	if username != "" {
		_ = logger.LogFileDownloadComplete(username, filename, len(keys))
	}

	return nil
}
