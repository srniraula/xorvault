package main

import (
	"bufio"
	"context"
	"dfs-project/dfspb"
	"dfs-project/pkg/config"
	"fmt"
	"log"
	"os"
	"strings"
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
	dfspb.UnimplementedMasterServerServer                                                       // Embedded type required by gRPC
	mu                                    sync.Mutex                                            // Protects fileChunks and fileSizes maps
	fileInfo                              map[string]map[string]map[int32]*dfspb.StripeMetadata // Maps username -> filename -> stripe_num -> StripeMetadata
	clientIDs                             map[string][]string                                   // username to filename map
	fileSizes                             map[string]map[string]int64                           // Maps username -> filename -> total file size in bytes
	chunkStatus                           map[string]string                                     // Maps chunk_id -> "PENDING"/"SUCCESS"
	chunkServers                          []string                                              // List of all known chunk server addresses
	servers                               map[string]*ServerInfo                                // Maps server address -> health status
	serversMu                             sync.RWMutex                                          // Protects servers map (RWMutex allows multiple readers)
	logger                                *log.Logger                                           // Custom logger for file output (logs to master.log)

	// Folder support
	clientFolders   map[string]map[string]bool  // Maps username -> folder_path -> exists
	fileUploadTimes map[string]map[string]int64 // Maps username -> filename -> unix_timestamp

	// WAL fields
	walFile   *os.File      // WAL file handle
	walWriter *bufio.Writer // Buffered writer for WAL
	walMu     sync.Mutex    // Protects WAL writes

	// High Availability
	IsStandby  bool       // If true, this is a passive master (reads WAL for updates)
	standbyMu  sync.Mutex // Protects startup/failover transition
	walOffset  int64      // Byte offset into WAL that standby has already replayed
	listenAddr string     // Own gRPC listen address (e.g. "0.0.0.0:50052") – written to .master_addr on promotion

	userPasswords map[string]string // Maps username -> password (simple 6-digit string)
}

// ensureClientMaps makes sure the per-client nested maps exist to avoid nil-map panics
func (m *MasterServer) ensureClientMaps(username string) {
	if _, ok := m.fileInfo[username]; !ok {
		m.fileInfo[username] = make(map[string]map[int32]*dfspb.StripeMetadata)
	}
	if _, ok := m.fileSizes[username]; !ok {
		m.fileSizes[username] = make(map[string]int64)
	}
	if _, ok := m.clientFolders[username]; !ok {
		m.clientFolders[username] = make(map[string]bool)
	}
	if _, ok := m.fileUploadTimes[username]; !ok {
		m.fileUploadTimes[username] = make(map[string]int64)
	}
	// clientIDs uses slices; append on nil is OK so no init required
}

// CreateFile registers a new file in the system
// Called by client before uploading - just records metadata, doesn't store data yet
// Parameters:
//   - filename: name of the file to create
//   - total_size: size of the entire file in bytes
func (m *MasterServer) CreateFile(ctx context.Context, req *dfspb.CreateFileRequest) (*dfspb.CreateFileResponse, error) {
	if m.IsStandby {
		return nil, fmt.Errorf("master is in STANDBY mode. Write operations are disabled")
	}

	m.mu.Lock()         // Lock to prevent concurrent modifications
	defer m.mu.Unlock() // Unlock when function returns

	if req.Username == "" {
		return nil, fmt.Errorf("username is required")
	}

	// ensure nested maps for this client exist
	m.ensureClientMaps(req.Username)

	// Verify password
	if pass, exists := m.userPasswords[req.Username]; !exists || pass != req.Password {
		return nil, fmt.Errorf("authentication failed: invalid username or password")
	}

	// Check if file already exists
	if _, ok := m.fileInfo[req.Username][req.Filename]; ok {
		return nil, fmt.Errorf("file %s already exists.", req.Filename)
	}

	// Log to WAL before updating in-memory state with benefit of data durability
	if err := m.LogCreateFileToWAL(req.Username, req.Filename, req.TotalSize); err != nil {
		return nil, err
	}

	//map client id with filename
	m.clientIDs[req.Username] = append(m.clientIDs[req.Username], req.Filename)

	// Initialize empty map for this filename
	m.fileInfo[req.Username][req.Filename] = make(map[int32]*dfspb.StripeMetadata)

	m.fileSizes[req.Username][req.Filename] = req.TotalSize

	// Track upload time
	m.fileUploadTimes[req.Username][req.Filename] = time.Now().Unix()

	// Allocate chunks and get the chunk-to-server mapping
	allocResp, err := m.allocateChunksInternal(req.Username, int(req.TotalSize), req.Filename)

	if err != nil {
		log.Printf("failed to allocate chunks: %v", err)
		return nil, err
	}

	m.logger.Printf("Created %s for user %s (%d bytes)", req.Filename, req.Username, req.TotalSize)
	return &dfspb.CreateFileResponse{
		Success:  true,
		Username: req.Username,
		Stripes:  allocResp.Stripes,
	}, nil
}

// AllocateChunk is the gRPC handler for chunk allocation requests
func (m *MasterServer) AllocateChunk(ctx context.Context, req *dfspb.AllocateChunkRequest) (*dfspb.AllocateChunkResponse, error) {
	if m.IsStandby {
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
func (m *MasterServer) allocateChunksInternal(username string, totalSize int, fileName string) (*dfspb.AllocateChunkResponse, error) {
	// Calculate how many chunks we'll need ==> (a+b-1)/b == ceil(a/b)
	// Formula: (fileSize + chunkSize - 1) / chunkSize handles partial last chunk
	totalChunks := (int(totalSize) + CHUNK_SIZE - 1) / CHUNK_SIZE

	// find number of chunks, DONE
	// find healthy chunkservers, DONE
	// and calculate chunk_ids and return map of
	// chunkservers: [chunk_ids] to client
	filename := fileName

	// Ensure client/map exists for storing stripe info
	m.ensureClientMaps(username)
	if _, ok := m.fileInfo[username][fileName]; !ok {
		m.fileInfo[username][fileName] = make(map[int32]*dfspb.StripeMetadata)
	}

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
	if len(healthy) < 3 {
		return nil, fmt.Errorf("insufficient healthy chunkservers: need 3, got %d", len(healthy))
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
		m.fileInfo[username][filename][int32(stripeNum)] = stripe
	}

	// Log chunk allocation to WAL with PENDING status
	// Store full stripe metadata for ]recovery
	walData := AllocateChunkData{
		Username: username,
		Filename: filename,
		Stripes:  m.fileInfo[username][filename],
		Status:   "PENDING",
	}

	if err := m.AppendWAL(OpAllocateChunk, walData); err != nil {
		m.logger.Printf("WAL append failed for AllocateChunk: %v", err)
		return nil, fmt.Errorf("failed to log to WAL: %v", err)
	}

	m.logger.Printf("Allocated chunks for %s (user: %s, status: PENDING):", filename, username)
	for stripeNum, stripe := range m.fileInfo[username][filename] {
		m.logger.Printf("  Stripe %d: chunks=%v, servers=%v", stripeNum, stripe.ChunkIds, stripe.Servers)
	}

	// Return stripe metadata directly
	return &dfspb.AllocateChunkResponse{
		Stripes: m.fileInfo[username][filename],
	}, nil
}

// GetFileMetadata returns information about a file needed for downloading
// Checks client ownership before returning metadata
func (m *MasterServer) GetFileMetadata(ctx context.Context, req *dfspb.GetFileMetadataRequest) (*dfspb.GetFileMetadataResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Verify password
	if pass, exists := m.userPasswords[req.Username]; !exists || pass != req.Password {
		return nil, fmt.Errorf("authentication failed: invalid username or password")
	}

	// Guard against missing client or file maps
	clientFiles, clientExists := m.fileInfo[req.Username]
	if !clientExists {
		return nil, fmt.Errorf("file not found: %s", req.Filename)
	}
	stripes, fileExists := clientFiles[req.Filename]
	if !fileExists || len(stripes) == 0 {
		return nil, fmt.Errorf("file not found: %s", req.Filename)
	}

	size := int64(0)
	if fs, ok := m.fileSizes[req.Username]; ok {
		size = fs[req.Filename]
	}

	// Check ownership: does this user own the file?
	ownedFiles, exists := m.clientIDs[req.Username]
	if !exists {
		return nil, fmt.Errorf("access denied: unknown username %s", req.Username)
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
		return nil, fmt.Errorf("access denied: user %s does not own file %s", req.Username, req.Filename)
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

		// Check if really new to chunkServers slice (avoid duplicates)
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
	if m.IsStandby {
		return &dfspb.ConfirmWriteResponse{Success: false}, fmt.Errorf("master is in STANDBY mode")
	}
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
	if m.IsStandby {
		return &dfspb.DeleteFileResponse{Success: false, Message: "master is in STANDBY mode"}, nil
	}
	m.mu.Lock()
	// Note: manually unlocking before checkpoint, no defer

	filename := req.Filename
	username := req.Username

	// Verify password
	if pass, exists := m.userPasswords[username]; !exists || pass != req.Password {
		m.mu.Unlock()
		return &dfspb.DeleteFileResponse{Success: false, Message: "authentication failed"}, nil
	}

	// Check if file exists (guard for missing client)
	clientFiles, clientExists := m.fileInfo[username]
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

	// Verify client ownership
	ownedFiles, clientExists := m.clientIDs[username]
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

	m.logger.Printf("Deleting %s for user %s: %d chunks across %d servers",
		filename, username, len(allChunkIDs), len(serverChunks))

	// Send DeleteChunks RPC to each chunk server
	totalDeleted := int32(0)
	for serverAddr, chunkIDs := range serverChunks {
		deleted, err := m.deleteChunksFromServer(serverAddr, chunkIDs, username)
		if err != nil {
			m.logger.Printf("Failed to delete chunks from %s: %v", serverAddr, err)
			// Continue with other servers even if one fails
		} else {
			totalDeleted += deleted
			m.logger.Printf("Deleted %d chunks from %s", deleted, serverAddr)
		}
	}

	// Update metadata - remove file info :: change logic here
	delete(m.fileInfo[username], filename)
	delete(m.fileSizes[username], filename)

	// Remove filename from clientIDs (but keep the client ID entry)
	updatedFiles := []string{}
	for _, f := range ownedFiles {
		if f != filename {
			updatedFiles = append(updatedFiles, f)
		}
	}
	m.clientIDs[username] = updatedFiles

	// Remove chunk statuses
	for _, chunkID := range allChunkIDs {
		delete(m.chunkStatus, chunkID)
	}

	// Log to WAL
	walData := DeleteFileData{
		Filename: filename,
		Username: username,
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
func (m *MasterServer) deleteChunksFromServer(serverAddr string, chunkIDs []string, username string) (int32, error) {
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

	// Verify password
	if pass, exists := m.userPasswords[req.Username]; !exists || pass != req.Password {
		return nil, fmt.Errorf("authentication failed")
	}

	files, exists := m.clientIDs[req.Username]
	if !exists {
		return &dfspb.ListFilesResponse{
			Filenames: []string{},
		}, nil
	}

	return &dfspb.ListFilesResponse{
		Filenames: files,
	}, nil
}

// Authenticate verifies user credentials or registers a new user
func (m *MasterServer) Authenticate(ctx context.Context, req *dfspb.AuthenticateRequest) (*dfspb.AuthenticateResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if req.IsRegister {
		if _, exists := m.userPasswords[req.Username]; exists {
			return &dfspb.AuthenticateResponse{Success: false, Message: "Username already exists"}, nil
		}
		if len(req.Password) != 6 {
			return &dfspb.AuthenticateResponse{Success: false, Message: "Password must be exactly 6 digits"}, nil
		}
		m.userPasswords[req.Username] = req.Password
		m.ensureClientMaps(req.Username)
		m.logger.Printf("New user registered: %s", req.Username)
		return &dfspb.AuthenticateResponse{Success: true, Message: "Registered successfully"}, nil
	}

	pass, exists := m.userPasswords[req.Username]
	if !exists {
		return &dfspb.AuthenticateResponse{Success: false, Message: "User does not exist"}, nil
	}
	if pass != req.Password {
		return &dfspb.AuthenticateResponse{Success: false, Message: "Incorrect password"}, nil
	}

	m.logger.Printf("User authenticated: %s", req.Username)
	return &dfspb.AuthenticateResponse{Success: true, Message: "Authenticated successfully"}, nil
}

// Ping is used by the secondary master (or others) to check health
func (m *MasterServer) Ping(ctx context.Context, req *dfspb.PingRequest) (*dfspb.PingResponse, error) {
	return &dfspb.PingResponse{Active: !m.IsStandby}, nil
}

// MonitorPrimary runs in a background goroutine on the Secondary Master
// It pings the Primary periodically. If Primary fails, Secondary promotes itself.
func (m *MasterServer) MonitorPrimary(primaryAddr string) {
	m.logger.Printf("Starting monitoring of Primary Master at %s", primaryAddr)

	ticker := time.NewTicker(2 * time.Second) // Ping every 2 seconds
	defer ticker.Stop()

	failCount := 0
	maxFails := 3 // Promote after 3 consecutive failures (approx 6 seconds)

	for range ticker.C {
		// Stop monitoring if we identify we are no longer standby (promoted)
		if !m.IsStandby {
			return
		}

		conn, err := grpc.NewClient(primaryAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			failCount++
			m.logger.Printf("Failed to connect to primary (attempt %d/%d): %v", failCount, maxFails, err)
		} else {
			client := dfspb.NewMasterServerClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			_, err := client.Ping(ctx, &dfspb.PingRequest{})
			cancel()
			conn.Close()

			if err != nil {
				failCount++
				m.logger.Printf("Primary ping failed (attempt %d/%d): %v", failCount, maxFails, err)
			} else {
				// Success, reset counter
				if failCount > 0 {
					m.logger.Printf("Primary recovered after %d failures", failCount)
				}
				failCount = 0
			}
		}

		if failCount >= maxFails {
			m.PromoteToActive()
			return
		}
	}
}

// PromoteToActive switches the server from Standby to Active mode.
// Before accepting writes it performs one final incremental WAL catch-up so
// no committed operation is lost, then advertises itself as the new primary
// by rewriting the ".master_addr" file with this device's real LAN IP.
func (m *MasterServer) PromoteToActive() {
	m.standbyMu.Lock()
	defer m.standbyMu.Unlock()

	if !m.IsStandby {
		return // Already active
	}

	m.logger.Println("!!! PRIMARY FAILURE DETECTED - PROMOTING TO ACTIVE !!!")

	// --- Final incremental WAL catch-up ---
	walName := "master.wal"
	if m.walFile != nil {
		walName = m.walFile.Name()
	}
	if err := m.RecoverFromWALIncremental(walName); err != nil {
		m.logger.Printf("Warning: final WAL catch-up error during promotion: %v", err)
	}

	// --- Switch mode ---
	m.IsStandby = false

	// --- Advertise self as primary via .master_addr ---
	// Extract the port from our listenAddr (e.g. "0.0.0.0:50052" → ":50052")
	portSuffix := ":50052"
	if idx := strings.LastIndex(m.listenAddr, ":"); idx >= 0 {
		portSuffix = m.listenAddr[idx:]
	}

	// Prefer explicit env var (set by start_secondary.sh via MASTER_ADDR),
	// then the .secondary_addr file, then auto-detect the LAN IP.
	var publicHost string
	if v := os.Getenv("SECONDARY_MASTER_IP"); v != "" {
		publicHost = v
	} else if data, err := os.ReadFile(".secondary_addr"); err == nil {
		// .secondary_addr contains "IP:port" — extract just the IP
		parts := strings.SplitN(strings.TrimSpace(string(data)), ":", 2)
		if len(parts) > 0 {
			publicHost = parts[0]
		}
	}
	if publicHost == "" {
		// Auto-detect LAN IP (works when all nodes are on the same machine for dev)
		if ip, err := config.GetLocalIP(); err == nil {
			publicHost = ip
		} else {
			publicHost = "127.0.0.1"
		}
	}

	publicAddr := publicHost + portSuffix
	if err := os.WriteFile(".master_addr", []byte(publicAddr+"\n"), 0644); err != nil {
		m.logger.Printf("Warning: could not update .master_addr: %v", err)
	} else {
		m.logger.Printf("Updated .master_addr → %s (chunk servers will follow on next heartbeat)", publicAddr)
	}

	m.logger.Println("Server is now ACTIVE and accepting write requests.")
}
