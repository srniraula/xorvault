package main

import (
	"bufio"
	"context"
	"dfs-project/dfspb"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// CHUNK_SIZE defines how large each chunk should be (1 MB = 1024 * 1024 bytes)
// Files are split into chunks of this size before uploading to chunk servers
const CHUNK_SIZE = 1 * 1024 * 1024

// ServerInfo tracks the health status of a chunk server
// Used to detect when chunk servers go offline
type ServerInfo struct {
	LastHeartbeat time.Time // Last time this server sent a heartbeat
	Alive         bool      // Whether the server is currently considered alive
}

// MasterServer is the brain of the distributed file system
// It keeps track of file metadata and chunk locations, but does NOT store actual file data
// Responsibilities:
//   - Track which files exist and their chunks
//   - Monitor chunk server health via heartbeats
//   - Allocate chunk IDs and decide where chunks should be stored
//   - Provide chunk locations to clients for reads/writes
//   - Log all operations to master.log file
type MasterServer struct {
	dfspb.UnimplementedMasterServerServer                                            // Embedded type required by gRPC
	mu                                    sync.Mutex                                 // Protects fileChunks and fileSizes maps
	fileInfo                              map[string]map[int32]*dfspb.StripeMetadata // Maps filename -> stripe_num -> StripeMetadata
	clientIDs                             map[int64][]string                         //client_id to filename map
	fileSizes                             map[string]int64                           // Maps filename -> total file size in bytes
	chunkStatus                           map[string]string                          // Maps chunk_id -> "PENDING"/"SUCCESS"
	chunkServers                          []string                                   // List of all known chunk server addresses
	servers                               map[string]*ServerInfo                     // Maps server address -> health status
	serversMu                             sync.RWMutex                               // Protects servers map (RWMutex allows multiple readers)
	logger                                *log.Logger                                // Custom logger for file output (logs to master.log)

	// WAL fields
	walFile   *os.File      // WAL file handle
	walWriter *bufio.Writer // Buffered writer for WAL
	walMu     sync.Mutex    // Protects WAL writes
}

// CreateFile registers a new file in the system
// Called by client before uploading - just records metadata, doesn't store data yet
// Parameters:
//   - filename: name of the file to create
//   - total_size: size of the entire file in bytes
func (m *MasterServer) CreateFile(ctx context.Context, req *dfspb.CreateFileRequest) (*dfspb.CreateFileResponse, error) {
	m.mu.Lock()         // Lock to prevent concurrent modifications
	defer m.mu.Unlock() // Unlock when function returns

	if req.ClientId == 0 {
		req.ClientId = RandomID()
	}

	// Log to WAL before updating in-memory state with benefit of data durability
	if err := m.LogCreateFileToWAL(req.ClientId, req.Filename, req.TotalSize); err != nil {
		return nil, err
	}

	//map client id with filename
	m.clientIDs[req.ClientId] = append(m.clientIDs[req.ClientId], req.Filename)
	// Initialize empty map for this filename
	m.fileInfo[req.Filename] = make(map[int32]*dfspb.StripeMetadata)

	m.fileSizes[req.Filename] = req.TotalSize

	// Allocate chunks and get the chunk-to-server mapping
	allocResp, err := m.allocateChunksInternal(int(req.TotalSize), req.Filename)
	if err != nil {
		log.Printf("failed to allocate chunks: %v", err)
		return nil, err
	}

	m.logger.Printf("Created %s (%d bytes)", req.Filename, req.TotalSize)
	return &dfspb.CreateFileResponse{
		Success:  true,
		ClientId: req.ClientId,
		Stripes:  allocResp.Stripes,
	}, nil
}

// AllocateChunk is the gRPC handler for chunk allocation requests
func (m *MasterServer) AllocateChunk(ctx context.Context, req *dfspb.AllocateChunkRequest) (*dfspb.AllocateChunkResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.allocateChunksInternal(int(req.FileSize), req.Filename)
}

// allocateChunksInternal assigns chunk IDs and selects chunk servers
// This is the internal implementation called by CreateFile
// NOTE: Caller must hold m.mu lock
// Returns:
//   - chunk allocation map: which chunks go to which servers
func (m *MasterServer) allocateChunksInternal(totalSize int, fileName string) (*dfspb.AllocateChunkResponse, error) {
	// Calculate how many chunks we'll need ==> (a+b-1)/b == ceil(a/b)
	// Formula: (fileSize + chunkSize - 1) / chunkSize handles partial last chunk
	totalChunks := (int(totalSize) + CHUNK_SIZE - 1) / CHUNK_SIZE

	// find number of chunks, DONE
	// find healthy chunkservers, DONE
	// and calculate chunk_ids and return map of
	// chunkservers: [chunk_ids] to client
	filename := fileName

	// Find all healthy (alive) chunk servers
	m.serversMu.RLock() // Read lock - multiple goroutines can read simultaneously
	var healthy []string
	for _, addr := range m.chunkServers {
		if info, ok := m.servers[addr]; ok && info.Alive {
			healthy = append(healthy, addr)
		}
	}
	m.serversMu.RUnlock()

	// If no servers are available, cannot store the chunk
	if len(healthy) == 0 {
		return nil, fmt.Errorf("no healthy chunkservers")
	}

	totalStripe := (totalChunks + 2 - 1) / 2
	chunkCounter := 1 // Global chunk counter

	// Generate chunk IDs and parity IDs for each stripe
	for stripeNum := 1; stripeNum <= totalStripe; stripeNum++ {
		// Build StripeMetadata for this stripe
		stripe := &dfspb.StripeMetadata{
			StripeNum: int32(stripeNum),
			ChunkIds:  make([]string, 3), // [data1, data2, parity]
			Servers:   make([]string, 3), // [server1, server2, server3]
		}

		// Create 2 data chunks per stripe (or less for last stripe)
		chunkIdx := 0
		for chunkInStripe := 1; chunkInStripe <= 2 && chunkCounter <= totalChunks; chunkInStripe++ {
			chunkID := fmt.Sprintf("%s_chunk%d_%04d", filename, stripeNum, chunkCounter)

			// Alternate between chunkserver1 and chunkserver2
			if chunkInStripe == 1 {
				stripe.ChunkIds[0] = chunkID
				stripe.Servers[0] = healthy[0]
			} else {
				stripe.ChunkIds[1] = chunkID
				stripe.Servers[1] = healthy[1]
			}

			// Mark chunk as PENDING
			m.chunkStatus[chunkID] = "PENDING"

			chunkCounter++
			chunkIdx++
		}

		// Create parity for this stripe
		parityID := fmt.Sprintf("%s_parity%d_%04d", filename, stripeNum, stripeNum)
		stripe.ChunkIds[2] = parityID
		stripe.Servers[2] = healthy[2]

		// Mark parity as PENDING
		m.chunkStatus[parityID] = "PENDING"

		// Store stripe metadata
		m.fileInfo[filename][int32(stripeNum)] = stripe
	}

	// Log chunk allocation to WAL with PENDING status
	// Store full stripe metadata for recovery
	walData := AllocateChunkData{
		Filename: filename,
		Stripes:  m.fileInfo[filename],
		Status:   "PENDING",
	}

	if err := m.AppendWAL(OpAllocateChunk, walData); err != nil {
		m.logger.Printf("WAL append failed for AllocateChunk: %v", err)
		return nil, fmt.Errorf("failed to log to WAL: %v", err)
	}

	m.logger.Printf("Allocated chunks for %s (status: PENDING):", filename)
	for stripeNum, stripe := range m.fileInfo[filename] {
		m.logger.Printf("  Stripe %d: chunks=%v, servers=%v", stripeNum, stripe.ChunkIds, stripe.Servers)
	}

	// Return stripe metadata directly
	return &dfspb.AllocateChunkResponse{
		Stripes: m.fileInfo[filename],
	}, nil
}

// GetFileMetadata returns information about a file needed for downloading
// Checks client ownership before returning metadata
func (m *MasterServer) GetFileMetadata(ctx context.Context, req *dfspb.GetFileMetadataRequest) (*dfspb.GetFileMetadataResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	stripes := m.fileInfo[req.Filename]
	size := m.fileSizes[req.Filename]

	// If file doesn't exist or has no chunks, return empty response
	if len(stripes) == 0 {
		return nil, fmt.Errorf("file not found: %s", req.Filename)
	}

	// Check ownership: does this client own the file?
	ownedFiles, exists := m.clientIDs[req.ClientId]
	if !exists {
		return nil, fmt.Errorf("access denied: unknown client ID %d", req.ClientId)
	}

	// Check if filename is in client's owned files
	fileOwned := false
	for _, ownedFile := range ownedFiles {
		if ownedFile == req.Filename {
			fileOwned = true
			break
		}
	}

	if !fileOwned {
		return nil, fmt.Errorf("access denied: client %d does not own file %s", req.ClientId, req.Filename)
	}

	// Return stripe metadata directly
	return &dfspb.GetFileMetadataResponse{
		Stripes:  stripes,
		FileSize: size,
	}, nil
}

// SendHeartbeat receives periodic "I'm alive" messages from chunk servers
// This is how the master knows which chunk servers are still running
// Chunk servers send heartbeats every 5 seconds
func (m *MasterServer) ReceiveHeartbeat(ctx context.Context, req *dfspb.HeartbeatRequest) (*dfspb.HeartbeatResponse, error) {
	addr := req.Address

	m.serversMu.Lock() // Write lock - only one goroutine can modify at a time
	// If this is a new chunk server we haven't seen before, register it
	if _, exists := m.servers[addr]; !exists {
		m.servers[addr] = &ServerInfo{}
		m.chunkServers = append(m.chunkServers, addr)
		m.logger.Printf("New chunkserver registered: %s", addr)
	}
	// Update the heartbeat timestamp and mark server as alive
	m.servers[addr].LastHeartbeat = time.Now()
	m.servers[addr].Alive = true
	m.serversMu.Unlock()

	m.logger.Printf("Heartbeat received from %s", addr)
	return &dfspb.HeartbeatResponse{Success: true}, nil
}

// ConfirmWrite marks chunks as successfully written after client confirmation
// This updates the WAL with SUCCESS status for uploaded chunks
func (m *MasterServer) ConfirmWrite(ctx context.Context, req *dfspb.ConfirmWriteRequest) (*dfspb.ConfirmWriteResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Update chunk status to SUCCESS
	successfulChunks := []string{}
	for _, chunkID := range req.ChunkIds {
		if _, exists := m.chunkStatus[chunkID]; exists {
			m.chunkStatus[chunkID] = "SUCCESS"
			successfulChunks = append(successfulChunks, chunkID)
		} else {
			m.logger.Printf("Warning: chunk %s not found in chunkStatus", chunkID)
		}
	}

	// Log to WAL
	walData := ConfirmWriteData{
		Filename: req.Filename,
		ChunkIDs: successfulChunks,
		Status:   "SUCCESS",
	}

	if err := m.AppendWAL(OpConfirmWrite, walData); err != nil {
		m.logger.Printf("WAL append failed for ConfirmWrite: %v", err)
		return &dfspb.ConfirmWriteResponse{Success: false}, fmt.Errorf("failed to log to WAL: %v", err)
	}

	m.logger.Printf("Confirmed write for %s: %d chunks marked SUCCESS", req.Filename, len(successfulChunks))

	return &dfspb.ConfirmWriteResponse{Success: true}, nil
}

// DeleteFile removes a file and all its chunks from the system
// Verifies client ownership before deletion
func (m *MasterServer) DeleteFile(ctx context.Context, req *dfspb.DeleteFileRequest) (*dfspb.DeleteFileResponse, error) {
	m.mu.Lock()
	// Note: manually unlocking before checkpoint, no defer

	filename := req.Filename
	clientID := req.ClientId

	// Check if file exists
	stripes, fileExists := m.fileInfo[filename]
	if !fileExists {
		return &dfspb.DeleteFileResponse{
			Success: false,
			Message: "file not found",
		}, nil
	}

	// Verify client ownership
	ownedFiles, clientExists := m.clientIDs[clientID]
	if !clientExists {
		return &dfspb.DeleteFileResponse{
			Success: false,
			Message: "file not found",
		}, nil
	}

	fileOwned := false
	for _, ownedFile := range ownedFiles {
		if ownedFile == filename {
			fileOwned = true
			break
		}
	}

	if !fileOwned {
		return &dfspb.DeleteFileResponse{
			Success: false,
			Message: "file not found",
		}, nil
	}

	// Group chunks by server address
	serverChunks := make(map[string][]string) // server_addr -> [chunk_ids]
	allChunkIDs := []string{}

	for _, stripe := range stripes {
		for i, chunkID := range stripe.ChunkIds {
			serverAddr := stripe.Servers[i]
			serverChunks[serverAddr] = append(serverChunks[serverAddr], chunkID)
			allChunkIDs = append(allChunkIDs, chunkID)
		}
	}

	m.logger.Printf("Deleting %s for client %d: %d chunks across %d servers",
		filename, clientID, len(allChunkIDs), len(serverChunks))

	// Send DeleteChunks RPC to each chunk server
	totalDeleted := int32(0)
	for serverAddr, chunkIDs := range serverChunks {
		deleted, err := m.deleteChunksFromServer(serverAddr, chunkIDs, clientID)
		if err != nil {
			m.logger.Printf("Failed to delete chunks from %s: %v", serverAddr, err)
			// Continue with other servers even if one fails
		} else {
			totalDeleted += deleted
			m.logger.Printf("Deleted %d chunks from %s", deleted, serverAddr)
		}
	}

	// Update metadata - remove file info
	delete(m.fileInfo, filename)
	delete(m.fileSizes, filename)

	// Remove filename from clientIDs (but keep the client ID entry)
	updatedFiles := []string{}
	for _, f := range ownedFiles {
		if f != filename {
			updatedFiles = append(updatedFiles, f)
		}
	}
	m.clientIDs[clientID] = updatedFiles

	// Remove chunk statuses
	for _, chunkID := range allChunkIDs {
		delete(m.chunkStatus, chunkID)
	}

	// Log to WAL
	walData := DeleteFileData{
		Filename: filename,
		ClientID: clientID,
	}

	if err := m.AppendWAL(OpDeleteFile, walData); err != nil {
		m.logger.Printf("WAL append failed for DeleteFile: %v", err)
		// Metadata already updated, just log the error
	}

	m.logger.Printf("Successfully deleted %s: %d/%d chunks removed", filename, totalDeleted, len(allChunkIDs))

	// Unlock before checkpoint to avoid deadlock (checkpoint also locks)
	m.mu.Unlock()

	// Trigger checkpoint
	if err := m.CreateCheckpoint("master.checkpoint"); err != nil {
		m.logger.Printf("Checkpoint creation failed after delete: %v", err)
	}

	return &dfspb.DeleteFileResponse{
		Success: true,
		Message: fmt.Sprintf("deleted %d chunks", totalDeleted),
	}, nil
}

// deleteChunksFromServer sends DeleteChunks RPC to a specific chunk server
func (m *MasterServer) deleteChunksFromServer(serverAddr string, chunkIDs []string, clientID int64) (int32, error) {
	// Connect to chunk server
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return 0, fmt.Errorf("failed to connect to %s: %v", serverAddr, err)
	}
	defer conn.Close()

	chunkClient := dfspb.NewChunkServerClient(conn)

	// Send delete request
	resp, err := chunkClient.DeleteChunks(context.Background(), &dfspb.DeleteChunksRequest{
		ChunkIds: chunkIDs,
		ClientId: clientID,
	})

	if err != nil {
		return 0, err
	}

	return resp.DeletedCount, nil
}

// ListFiles returns all files uploaded by the given client
func (m *MasterServer) ListFiles(ctx context.Context, req *dfspb.ListFilesRequest) (*dfspb.ListFilesResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	files, exists := m.clientIDs[req.ClientId]
	if !exists {
		return &dfspb.ListFilesResponse{
			Filenames: []string{},
		}, nil
	}

	return &dfspb.ListFilesResponse{
		Filenames: files,
	}, nil
}
