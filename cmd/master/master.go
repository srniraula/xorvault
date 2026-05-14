package main

import (
	"bufio"
	"context"
	"dfs-project/dfspb"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"sync"
	"time"

	"dfs-project/pkg/config"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// CHUNK_SIZE is the size of each data chunk. Edit pkg/config/config.go to change it.
const CHUNK_SIZE = config.ChunkSize

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
	dfspb.UnimplementedMasterServerServer                                                      // Embedded type required by gRPC
	mu                                    sync.Mutex                                           // Protects fileChunks and fileSizes maps
	fileInfo                              map[int64]map[string]map[int32]*dfspb.StripeMetadata // Maps filename -> stripe_num -> StripeMetadata
	clientIDs                             map[int64][]string                                   //client_id to filename map
	fileSizes                             map[int64]map[string]int64                           // Maps filename -> total file size in bytes
	chunkStatus                           map[string]string                                    // Maps chunk_id -> "PENDING"/"SUCCESS"
	chunkServers                          []string                                             // List of all known chunk server addresses
	servers                               map[string]*ServerInfo                               // Maps server address -> health status
	serversMu                             sync.RWMutex                                         // Protects servers map (RWMutex allows multiple readers)
	logger                                *log.Logger                                          // Custom logger for file output (logs to master.log)

	// Folder support
	clientFolders   map[int64]map[string]bool  // Maps client_id -> folder_path -> exists
	fileUploadTimes map[int64]map[string]int64 // Maps client_id -> filename -> unix_timestamp
	clientUsernames map[int64]string           // Maps client_id -> username for directory naming

	// WAL fields
	walFile   *os.File      // WAL file handle
	walWriter *bufio.Writer // Buffered writer for WAL
	walMu     sync.Mutex    // Protects WAL writes
	walOffset int64         // Byte offset for incremental WAL replay (standby)

	// Failover fields
	secondaryAddr string // address of the node we replicate to (set to peerAddr when primary)
	myAddr        string // this instance's own address, e.g. "192.168.1.10:50051"
	peerAddr      string // permanent address of the other master node (never changes)
	walSeq        uint64 // monotonically increasing WAL sequence number
	isPrimary     bool   // true if this instance is the active primary
	generation    uint64 // epoch counter: incremented every time a new master promotes itself

	// Persistent replication client to avoid per-WAL dial churn under high write concurrency.
	replMu           sync.Mutex
	secondaryConn    *grpc.ClientConn
	secondaryClient  dfspb.SecondaryMasterServerClient
	secondaryConnFor string
}

// ensureClientMaps makes sure the per-client nested maps exist to avoid nil-map panics
func (m *MasterServer) ensureClientMaps(clientID int64) {
	if _, ok := m.fileInfo[clientID]; !ok {
		m.fileInfo[clientID] = make(map[string]map[int32]*dfspb.StripeMetadata)
	}
	if _, ok := m.fileSizes[clientID]; !ok {
		m.fileSizes[clientID] = make(map[string]int64)
	}
	if _, ok := m.clientFolders[clientID]; !ok {
		m.clientFolders[clientID] = make(map[string]bool)
	}
	if _, ok := m.fileUploadTimes[clientID]; !ok {
		m.fileUploadTimes[clientID] = make(map[string]int64)
	}
	// clientIDs uses slices; append on nil is OK so no init required
	// clientUsernames is a flat map; no nested init needed
}

// CreateFile registers a new file in the system
// Called by client before uploading - just records metadata, doesn't store data yet
// Parameters:
//   - filename: name of the file to create
//   - total_size: size of the entire file in bytes
func (m *MasterServer) CreateFile(ctx context.Context, req *dfspb.CreateFileRequest) (*dfspb.CreateFileResponse, error) {
	if !m.isPrimary {
		return nil, fmt.Errorf("master is in STANDBY mode. Write operations are disabled")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if req.ClientId == 0 {
		req.ClientId = RandomID()
	}

	m.ensureClientMaps(req.ClientId)

	if req.Username != "" {
		m.clientUsernames[req.ClientId] = req.Username
	}

	if _, ok := m.fileInfo[req.ClientId][req.Filename]; ok {
		return nil, fmt.Errorf("file %s already exists.", req.Filename)
	}

	if err := m.LogCreateFileToWAL(req.ClientId, req.Filename, req.TotalSize); err != nil {
		return nil, err
	}

	m.clientIDs[req.ClientId] = append(m.clientIDs[req.ClientId], req.Filename)

	m.fileInfo[req.ClientId][req.Filename] = make(map[int32]*dfspb.StripeMetadata)

	m.fileSizes[req.ClientId][req.Filename] = req.TotalSize

	m.fileUploadTimes[req.ClientId][req.Filename] = time.Now().Unix()

	allocResp, err := m.allocateChunksInternal(int64(req.ClientId), int(req.TotalSize), req.Filename)

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
	if !m.isPrimary {
		return nil, fmt.Errorf("master is in STANDBY mode. Write operations are disabled")
	}
	// TODO: Update allocateChunksInternal to not rely on CreateFile params if used directly
	return nil, fmt.Errorf("AllocateChunk RPC not directly implemented yet (use CreateFile)")
}

// allocateChunksInternal assigns chunk IDs and selects chunk servers
// This is the internal implementation called by CreateFile
// NOTE: Caller must hold m.mu lock
// Returns:
//   - chunk allocation map: which chunks go to which servers
func (m *MasterServer) allocateChunksInternal(clientID int64, totalSize int, fileName string) (*dfspb.AllocateChunkResponse, error) {
	totalChunks := (int(totalSize) + CHUNK_SIZE - 1) / CHUNK_SIZE

	filename := fileName

	m.ensureClientMaps(clientID)
	if _, ok := m.fileInfo[clientID][fileName]; !ok {
		m.fileInfo[clientID][fileName] = make(map[int32]*dfspb.StripeMetadata)
	}

	m.serversMu.RLock()
	var healthy []string
	for _, addr := range m.chunkServers {
		if info, ok := m.servers[addr]; ok && info.Alive {
			healthy = append(healthy, addr)
		}
	}
	m.serversMu.RUnlock()

	if len(healthy) < 3 {
		return nil, fmt.Errorf("insufficient healthy chunkservers: need 3, got %d", len(healthy))
	}

	sort.Strings(healthy)

	totalStripe := (totalChunks + 2 - 1) / 2
	chunkCounter := 1

	for stripeNum := 1; stripeNum <= totalStripe; stripeNum++ {
		stripe := &dfspb.StripeMetadata{
			StripeNum: int32(stripeNum),
			ChunkIds:  make([]string, 3),
			Servers:   make([]string, 3),
		}

		chunkIdx := 0
		for chunkInStripe := 1; chunkInStripe <= 2 && chunkCounter <= totalChunks; chunkInStripe++ {
			chunkID := fmt.Sprintf("%s_chunk%d_%04d", filename, stripeNum, chunkCounter)

			stripe.ChunkIds[chunkInStripe-1] = chunkID
			stripe.Servers[chunkInStripe-1] = healthy[chunkInStripe-1]

			m.chunkStatus[chunkID] = "PENDING"

			chunkCounter++
			chunkIdx++
		}

		parityID := fmt.Sprintf("%s_parity%d_%04d", filename, stripeNum, stripeNum)
		stripe.ChunkIds[2] = parityID
		stripe.Servers[2] = healthy[2]

		m.chunkStatus[parityID] = "PENDING"

		m.fileInfo[clientID][filename][int32(stripeNum)] = stripe
	}

	walData := AllocateChunkData{
		ClientID: clientID,
		Filename: filename,
		Stripes:  m.fileInfo[clientID][filename],
		Status:   "PENDING",
	}

	if err := m.AppendWAL(OpAllocateChunk, walData); err != nil {
		m.logger.Printf("WAL append failed for AllocateChunk: %v", err)
		return nil, fmt.Errorf("failed to log to WAL: %v", err)
	}

	m.logger.Printf("Allocated chunks for %s (status: PENDING):", filename)
	for stripeNum, stripe := range m.fileInfo[clientID][filename] {
		m.logger.Printf("  Stripe %d: chunks=%v, servers=%v", stripeNum, stripe.ChunkIds, stripe.Servers)
	}

	return &dfspb.AllocateChunkResponse{
		Stripes: m.fileInfo[clientID][filename],
	}, nil
}

// GetFileMetadata returns information about a file needed for downloading
// Checks client ownership before returning metadata
func (m *MasterServer) GetFileMetadata(ctx context.Context, req *dfspb.GetFileMetadataRequest) (*dfspb.GetFileMetadataResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	clientFiles, clientExists := m.fileInfo[req.ClientId]
	if !clientExists {
		return nil, fmt.Errorf("file not found: %s", req.Filename)
	}
	stripes, fileExists := clientFiles[req.Filename]
	if !fileExists || len(stripes) == 0 {
		return nil, fmt.Errorf("file not found: %s", req.Filename)
	}

	size := int64(0)
	if fs, ok := m.fileSizes[req.ClientId]; ok {
		size = fs[req.Filename]
	}

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

	m.serversMu.Lock()
	if _, exists := m.servers[addr]; !exists {
		m.servers[addr] = &ServerInfo{}

		found := false
		for _, s := range m.chunkServers {
			if s == addr {
				found = true
				break
			}
		}
		if !found {
			m.chunkServers = append(m.chunkServers, addr)
		}

		m.logger.Printf("New chunkserver registered: %s", addr)
	}
	m.servers[addr].LastHeartbeat = time.Now()
	m.servers[addr].Alive = true
	m.serversMu.Unlock()

	m.logger.Printf("Heartbeat received from %s", addr)
	return &dfspb.HeartbeatResponse{Success: true}, nil
}

// ConfirmWrite marks chunks as successfully written after client confirmation
// This updates the WAL with SUCCESS status for uploaded chunks
func (m *MasterServer) ConfirmWrite(ctx context.Context, req *dfspb.ConfirmWriteRequest) (*dfspb.ConfirmWriteResponse, error) {
	if !m.isPrimary {
		return &dfspb.ConfirmWriteResponse{Success: false}, fmt.Errorf("master is in STANDBY mode")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	successfulChunks := []string{}
	for _, chunkID := range req.ChunkIds {
		if _, exists := m.chunkStatus[chunkID]; exists {
			m.chunkStatus[chunkID] = "SUCCESS"
			successfulChunks = append(successfulChunks, chunkID)
		} else {
			m.logger.Printf("Warning: chunk %s not found in chunkStatus", chunkID)
		}
	}

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
	if !m.isPrimary {
		return &dfspb.DeleteFileResponse{Success: false, Message: "master is in STANDBY mode"}, nil
	}
	m.mu.Lock()

	filename := req.Filename
	clientID := req.ClientId

	clientFiles, clientExists := m.fileInfo[clientID]
	if !clientExists {
		m.mu.Unlock()
		return &dfspb.DeleteFileResponse{
			Success: false,
			Message: "this file is not found",
		}, nil
	}

	stripes, fileExists := clientFiles[filename]
	if !fileExists {
		m.mu.Unlock()
		return &dfspb.DeleteFileResponse{
			Success: false,
			Message: "this file is not found",
		}, nil
	}

	ownedFiles, clientExists := m.clientIDs[clientID]
	if !clientExists {
		m.mu.Unlock()
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
		m.mu.Unlock()
		return &dfspb.DeleteFileResponse{
			Success: false,
			Message: "file not found",
		}, nil
	}

	serverChunks := make(map[string][]string)
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

	username := m.clientUsernames[clientID]

	totalDeleted := int32(0)
	for serverAddr, chunkIDs := range serverChunks {
		deleted, err := m.deleteChunksFromServer(serverAddr, chunkIDs, clientID, username)
		if err != nil {
			m.logger.Printf("Failed to delete chunks from %s: %v", serverAddr, err)
		} else {
			totalDeleted += deleted
			m.logger.Printf("Deleted %d chunks from %s", deleted, serverAddr)
		}
	}

	delete(m.fileInfo[clientID], filename)
	delete(m.fileSizes[clientID], filename)

	updatedFiles := []string{}
	for _, f := range ownedFiles {
		if f != filename {
			updatedFiles = append(updatedFiles, f)
		}
	}
	m.clientIDs[clientID] = updatedFiles

	for _, chunkID := range allChunkIDs {
		delete(m.chunkStatus, chunkID)
	}

	walData := DeleteFileData{
		Filename: filename,
		ClientID: clientID,
	}

	if err := m.AppendWAL(OpDeleteFile, walData); err != nil {
		m.logger.Printf("WAL append failed for DeleteFile: %v", err)
	}

	m.logger.Printf("Successfully deleted %s: %d/%d chunks removed", filename, totalDeleted, len(allChunkIDs))

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
func (m *MasterServer) deleteChunksFromServer(serverAddr string, chunkIDs []string, clientID int64, username string) (int32, error) {
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return 0, fmt.Errorf("failed to connect to %s: %v", serverAddr, err)
	}
	defer conn.Close()

	chunkClient := dfspb.NewChunkServerClient(conn)

	resp, err := chunkClient.DeleteChunks(context.Background(), &dfspb.DeleteChunksRequest{
		ChunkIds: chunkIDs,
		ClientId: clientID,
		Username: username,
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

// GetActiveMaster lets any node (client, chunk server) discover the active master.
// Returns this node's own address and whether it is currently the primary.
// Both primary and standby implement this identically — the caller uses
// IsPrimary to know if it needs to try the other address.
func (m *MasterServer) GetActiveMaster(ctx context.Context, req *dfspb.GetActiveMasterRequest) (*dfspb.GetActiveMasterResponse, error) {
	return &dfspb.GetActiveMasterResponse{
		ActiveMasterAddr: m.myAddr,
		IsPrimary:        m.isPrimary,
	}, nil
}

// getOrCreateSecondaryClient returns a cached client for secondaryAddr, creating it when needed.
func (m *MasterServer) getOrCreateSecondaryClient(secondaryAddr string) (dfspb.SecondaryMasterServerClient, error) {
	m.replMu.Lock()
	defer m.replMu.Unlock()

	if m.secondaryClient != nil && m.secondaryConn != nil && m.secondaryConnFor == secondaryAddr {
		return m.secondaryClient, nil
	}

	if m.secondaryConn != nil {
		_ = m.secondaryConn.Close()
		m.secondaryConn = nil
		m.secondaryClient = nil
		m.secondaryConnFor = ""
	}

	conn, err := grpc.NewClient(secondaryAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	m.secondaryConn = conn
	m.secondaryClient = dfspb.NewSecondaryMasterServerClient(conn)
	m.secondaryConnFor = secondaryAddr
	return m.secondaryClient, nil
}

// resetSecondaryClient closes and clears the cached secondary client so next use re-dials.
func (m *MasterServer) resetSecondaryClient() {
	m.replMu.Lock()
	defer m.replMu.Unlock()

	if m.secondaryConn != nil {
		_ = m.secondaryConn.Close()
	}
	m.secondaryConn = nil
	m.secondaryClient = nil
	m.secondaryConnFor = ""
}

// replicateWALToSecondary sends a single WAL entry to the secondary master.
// Called synchronously from AppendWAL (after releasing walMu) with bounded deadlines and one retry.
// Errors are logged but do NOT fail the primary operation.
func (m *MasterServer) replicateWALToSecondary(entry WALEntry, seq uint64) {
	if m.secondaryAddr == "" {
		return
	}

	// Marshal first — fail fast before making a network connection
	payload, err := json.Marshal(entry)
	if err != nil {
		m.logger.Printf("WAL replication: marshal failed: %v", err)
		return
	}

	// Retry once on transient channel failures. Keep bounded timeout per attempt.
	for attempt := 1; attempt <= 2; attempt++ {
		client, err := m.getOrCreateSecondaryClient(m.secondaryAddr)
		if err != nil {
			m.logger.Printf("WAL replication: cannot connect to secondary %s (attempt %d/2): %v", m.secondaryAddr, attempt, err)
			m.resetSecondaryClient()
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		resp, err := client.ReplicateWAL(ctx, &dfspb.ReplicateWALRequest{
			Entry: &dfspb.WALEntry{
				SequenceNumber: seq,
				EntryType:      walEntryTypeFromOp(entry.Operation),
				Payload:        payload,
				TimestampUnix:  entry.Timestamp,
			},
		}, grpc.WaitForReady(true))
		cancel()

		if err != nil {
			m.logger.Printf("WAL replication RPC failed (seq %d, attempt %d/2): %v", seq, attempt, err)
			m.resetSecondaryClient()
			continue
		}

		m.logger.Printf("WAL seq %d replicated (secondary ack=%d)", seq, resp.LastSequenceAck)
		return
	}

	m.logger.Printf("WAL replication failed after retries (seq %d)", seq)
}

// walEntryTypeFromOp converts our string op constants to the proto WALEntryType enum.
func walEntryTypeFromOp(op string) dfspb.WALEntryType {
	switch op {
	case OpCreateFile:
		return dfspb.WALEntryType_WAL_CREATE_FILE
	case OpConfirmWrite:
		return dfspb.WALEntryType_WAL_CONFIRM_WRITE
	case OpDeleteFile:
		return dfspb.WALEntryType_WAL_DELETE_FILE
	default:
		return dfspb.WALEntryType_WAL_CREATE_FILE
	}
}
