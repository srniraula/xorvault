// Provide a reusable go client API for HIGL-LEVEL DFS operations so the http
// server can call DFS functions without dealing with low-level gRPC details.
package dfsclient

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"dfs-project/dfspb"
	"dfs-project/pkg/config"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const CHUNK_SIZE = 1 * 1024 * 1024

// Interface exposes high-level DFS operations used by HTTP handlers
type Client interface {
	Authenticate(ctx context.Context, username, password string, isRegister bool) (bool, string, error)
	ListFiles(ctx context.Context, username, password string) ([]string, error)
	DeleteFile(ctx context.Context, username, password string, filename string) (int, error)
	UploadFile(ctx context.Context, username, password string, filename string, data io.Reader, size int64) (string, error)
	DownloadFile(ctx context.Context, username, password string, filename string, destPath string) error
}

// GrpcClient implements Client using the existing gRPC Master/ChunkServer APIs
// It supports automatic failover between primary and secondary masters.
type GrpcClient struct {
	primaryAddr   string
	secondaryAddr string

	mu          sync.Mutex
	currentConn *grpc.ClientConn
	masterCli   dfspb.MasterServerClient
}

// NewGrpcClient connects to the configured master addresses and returns a client
func NewGrpcClient(masterAddr string) (*GrpcClient, error) {
	if masterAddr == "" {
		masterAddr = config.GetMasterAddr()
	}
	secondary := config.GetSecondaryMasterAddr()

	g := &GrpcClient{
		primaryAddr:   masterAddr,
		secondaryAddr: secondary,
	}

	// Try to establish initial connection
	_, err := g.getConn(context.Background())
	if err != nil {
		// We return the client anyway; it will try to reconnect on the first real call
		fmt.Printf("Warning: initial connection to masters failed: %v. Will retry on demand.\n", err)
	}

	return g, nil
}

func (g *GrpcClient) getConn(ctx context.Context) (dfspb.MasterServerClient, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.masterCli != nil {
		return g.masterCli, nil
	}

	// Build list of all known master addresses for active probing
	var targets []string

	// Check local .master_addr for a hint (useful on same machine or shared FS)
	if data, err := os.ReadFile(".master_addr"); err == nil {
		active := strings.TrimSpace(string(data))
		if active != "" {
			targets = append(targets, active)
		}
	}

	// Add primary and secondary (avoiding duplicates)
	addUnique := func(addr string) {
		if addr == "" {
			return
		}
		for _, existing := range targets {
			if existing == addr {
				return
			}
		}
		targets = append(targets, addr)
	}
	addUnique(g.primaryAddr)
	addUnique(g.secondaryAddr)

	if len(targets) == 0 {
		return nil, fmt.Errorf("no master addresses configured")
	}

	var lastErr error
	for _, addr := range targets {
		conn, err := grpc.Dial(addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithConnectParams(grpc.ConnectParams{MinConnectTimeout: 2 * time.Second}),
		)
		if err != nil {
			lastErr = fmt.Errorf("dial %s: %w", addr, err)
			continue
		}

		// Actually validate the connection works with a lightweight ListFiles call
		// (Using empty username/password just to test connectivity - will fail auth but proves master is up)
		cli := dfspb.NewMasterServerClient(conn)
		testCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, err = cli.ListFiles(testCtx, &dfspb.ListFilesRequest{Username: "__probe__", Password: ""})
		cancel()

		// Any response (even auth failure) means master is reachable
		// Only network/unavailable errors indicate master is down
		if err != nil && isConnectionError(err) {
			lastErr = fmt.Errorf("probe %s: %w", addr, err)
			conn.Close()
			continue
		}

		// Found a working master!
		g.currentConn = conn
		g.masterCli = cli
		return g.masterCli, nil
	}

	return nil, fmt.Errorf("could not connect to any master (tried %v): %v", targets, lastErr)
}

// isConnectionError returns true if the error indicates the server is unreachable
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Check for common gRPC connection failure patterns
	return strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "unavailable") ||
		strings.Contains(errStr, "no connection") ||
		strings.Contains(errStr, "connection error") ||
		strings.Contains(errStr, "transport") ||
		strings.Contains(errStr, "deadline exceeded")
}

func (g *GrpcClient) resetConn() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.currentConn != nil {
		g.currentConn.Close()
	}
	g.currentConn = nil
	g.masterCli = nil
}

func (g *GrpcClient) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.currentConn != nil {
		return g.currentConn.Close()
	}
	return nil
}

func (g *GrpcClient) Authenticate(ctx context.Context, username, password string, isRegister bool) (bool, string, error) {
	cli, err := g.getConn(ctx)
	if err != nil {
		return false, "", err
	}

	resp, err := cli.Authenticate(ctx, &dfspb.AuthenticateRequest{
		Username:   username,
		Password:   password,
		IsRegister: isRegister,
	})
	if err != nil {
		g.resetConn()
		// Retry once
		cli, err = g.getConn(ctx)
		if err != nil {
			return false, "", err
		}
		resp, err = cli.Authenticate(ctx, &dfspb.AuthenticateRequest{Username: username, Password: password, IsRegister: isRegister})
		if err != nil {
			return false, "", err
		}
	}
	return resp.Success, resp.Message, nil
}

func (g *GrpcClient) ListFiles(ctx context.Context, username, password string) ([]string, error) {
	cli, err := g.getConn(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := cli.ListFiles(ctx, &dfspb.ListFilesRequest{Username: username, Password: password})
	if err != nil {
		g.resetConn()
		cli, err = g.getConn(ctx)
		if err != nil {
			return nil, err
		}
		resp, err = cli.ListFiles(ctx, &dfspb.ListFilesRequest{Username: username, Password: password})
		if err != nil {
			return nil, err
		}
	}
	return resp.Filenames, nil
}

func (g *GrpcClient) DeleteFile(ctx context.Context, username, password string, filename string) (int, error) {
	cli, err := g.getConn(ctx)
	if err != nil {
		return 0, err
	}

	resp, err := cli.DeleteFile(ctx, &dfspb.DeleteFileRequest{Username: username, Password: password, Filename: filename})
	if err != nil {
		g.resetConn()
		cli, err = g.getConn(ctx)
		if err != nil {
			return 0, err
		}
		resp, err = cli.DeleteFile(ctx, &dfspb.DeleteFileRequest{Username: username, Password: password, Filename: filename})
		if err != nil {
			return 0, err
		}
	}
	if !resp.Success {
		return 0, fmt.Errorf("delete failed: %s", resp.Message)
	}
	// Parse deleted chunks count from message if present
	var deleted int
	_, err = fmt.Sscanf(resp.Message, "deleted %d chunks", &deleted)
	if err != nil {
		// couldn't parse, but deletion succeeded
		return 0, nil
	}
	return deleted, nil
}

// UploadFile uploads content from reader to the DFS and returns the assigned username
func (g *GrpcClient) UploadFile(ctx context.Context, username, password string, filename string, data io.Reader, size int64) (string, error) {
	cli, err := g.getConn(ctx)
	if err != nil {
		return "", err
	}

	// Create file (username must be provided)
	createReq := &dfspb.CreateFileRequest{Filename: filename, TotalSize: size, Username: username, Password: password}
	createResp, err := cli.CreateFile(ctx, createReq)
	if err != nil {
		g.resetConn()
		cli, err = g.getConn(ctx)
		if err != nil {
			return "", err
		}
		createResp, err = cli.CreateFile(ctx, createReq)
		if err != nil {
			return "", fmt.Errorf("CreateFile failed: %w", err)
		}
	}
	assignedUser := createResp.Username
	// Build stripes map
	stripesMap := createResp.Stripes

	// Stream file in stripes and upload using pipeline similar to cmd/client
	stripeChan := make(chan Stripe, 2)
	errChan := make(chan error, 1)

	go g.streamFileInStripes(data, stripesMap, stripeChan, errChan)

	// ACK queue tracks pending chunks
	ack := NewAckQueue()

	// start uploading stripes as they arrive
	successfulChunks, err := g.uploadStripesStreaming(stripeChan, ack, assignedUser)
	if err != nil {
		return assignedUser, err
	}

	// Check producer errors
	select {
	case err := <-errChan:
		if err != nil {
			return assignedUser, err
		}
	default:
	}

	// Confirm write to master
	if len(successfulChunks) > 0 {
		_, err := cli.ConfirmWrite(ctx, &dfspb.ConfirmWriteRequest{Filename: filename, ChunkIds: successfulChunks})
		if err != nil {
			// Try once more with fresh connection
			g.resetConn()
			cli, _ = g.getConn(ctx)
			if cli != nil {
				_, err = cli.ConfirmWrite(ctx, &dfspb.ConfirmWriteRequest{Filename: filename, ChunkIds: successfulChunks})
			}
			if err != nil {
				return assignedUser, fmt.Errorf("confirm write failed: %w", err)
			}
		}
	}

	return assignedUser, nil
}

func writeChunkToServer(ctx context.Context, serverAddr string, chunkID string, data []byte, username string) error {
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
	_, err = chunkCli.WriteChunk(ctx, &dfspb.WriteChunkRequest{ChunkId: chunkID, Data: data, Checksum: checksum, Username: username})
	if err != nil {
		return err
	}
	return nil
}

// chunkUploader is a test hook; by default points to real writeChunkToServer implementation
var chunkUploader = writeChunkToServer

// chunkDownloader is a test hook for download; by default calls the real method
var chunkDownloader = func(g *GrpcClient, chunkID, serverAddr string, username string, isData1, isData2, isParity bool) DownloadedChunk {
	return g.downloadChunkFromServer(chunkID, serverAddr, username, isData1, isData2, isParity)
}

// checksum implementation moved to checksum.go

// helper functions and types were moved to dedicated files under pkg/dfsclient

// DownloadFile downloads a file and writes to destPath
func (g *GrpcClient) DownloadFile(ctx context.Context, username, password string, filename string, destPath string) error {
	cli, err := g.getConn(ctx)
	if err != nil {
		return err
	}

	// get metadata
	getReq := &dfspb.GetFileMetadataRequest{Username: username, Password: password, Filename: filename}
	meta, err := cli.GetFileMetadata(ctx, getReq)
	if err != nil {
		g.resetConn()
		cli, err = g.getConn(ctx)
		if err != nil {
			return err
		}
		meta, err = cli.GetFileMetadata(ctx, getReq)
		if err != nil {
			return fmt.Errorf("GetFileMetadata failed: %w", err)
		}
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

		sd := g.downloadStripe(info, username)

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

	return nil
}
