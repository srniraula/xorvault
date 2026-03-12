// package main

// import (
// 	"bufio"
// 	"dfs-project/dfspb"
// 	"dfs-project/pkg/config"
// 	"google.golang.org/grpc"
// 	"log"
// 	"net"
// 	"os"
// 	"time"
// )

// // main starts the master server and background health monitoring
// func main() {
// 	// Setup log file for Master - all logs will be written to master.log
// 	logFile, err := os.OpenFile("log_files/master.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
// 	if err != nil {
// 		log.Fatalf("Failed to open master.log: %v", err)
// 	}
// 	defer logFile.Close()

// 	// Create custom logger with prefix "MASTER: " and timestamp
// 	masterLogger := log.New(logFile, "MASTER: ", log.LstdFlags|log.Lshortfile)

// 	// Replace default log with file logger (for fatal errors)
// 	log.SetOutput(logFile)

// 	// Start listening on port 50051 for incoming gRPC requests
// 	lis, err := net.Listen("tcp", "0.0.0.0:50051")
// 	if err != nil {
// 		log.Fatalf("Failed to listen: %v", err)
// 	}

// 	// Create gRPC server instance
// 	s := grpc.NewServer()

// 	// Open WAL file for write-ahead logging
// 	walFile, err := os.OpenFile("master.wal", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
// 	if err != nil {
// 		log.Fatalf("Failed to open WAL file: %v", err)
// 	}
// 	defer walFile.Close()

// 	// Initialize the MasterServer with empty maps and known chunk server addresses
// 	server := &MasterServer{
// 		fileInfo:     make(map[int64]map[string]map[int32]*dfspb.StripeMetadata),
// 		clientIDs:    make(map[int64][]string),
// 		fileSizes:    make(map[int64]map[string]int64),
// 		chunkStatus:  make(map[string]string),      // Empty chunk status map
// 		chunkServers: config.GetChunkServers(),     // Get chunk servers from config (Docker-aware)
// 		servers:      make(map[string]*ServerInfo), // Empty server health map
// 		logger:       masterLogger,                 // Custom logger for file output
// 		walFile:      walFile,                      // WAL file handle
// 		walWriter:    bufio.NewWriter(walFile),     // Buffered WAL writer
// 	}

// 	// Restore from checkpoint first (if exists)
// 	if err := server.LoadCheckpoint("master.checkpoint"); err != nil {
// 		log.Fatalf("Checkpoint loading failed: %v", err)
// 	}

// 	// Then replay WAL entries after checkpoint
// 	if err := server.RecoverFromWAL("master.wal"); err != nil {
// 		log.Fatalf("WAL recovery failed: %v", err)
// 	}
// 	// Register our MasterServer to handle gRPC requests
// 	dfspb.RegisterMasterServerServer(s, server)

// 	// Start background goroutine for periodic checkpointing (every 5 minutes)
// 	go server.PeriodicCheckpoint(5, "master.checkpoint", "master.wal")

// 	// Start background goroutine for dead server detection
// 	// This runs continuously in the background checking for dead servers
// 	go func() {
// 		for {
// 			time.Sleep(10 * time.Second) // Check every 10 seconds

// 			server.serversMu.Lock()
// 			for addr, info := range server.servers {
// 				// If no heartbeat received in 20+ seconds, mark server as dead
// 				if time.Since(info.LastHeartbeat) > 20*time.Second {
// 					if info.Alive {
// 						info.Alive = false
// 						server.logger.Printf("DEAD SERVER DETECTED: %s", addr)
// 					}
// 				}
// 			}
// 			server.serversMu.Unlock()
// 		}
// 	}()

// 	log.Println("Master running on :50051 – Logs to master.log")

// 	// Start serving - this blocks until server shuts down
// 	if err := s.Serve(lis); err != nil {
// 		log.Fatalf("Failed to serve: %v", err)
// 	}
// }

package main

import (
	"bufio"
	"context"
	"dfs-project/dfspb"
	"dfs-project/pkg/config"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// main starts the master server and background health monitoring
func main() {
	// --- command-line flags for failover ---
	peerAddr := flag.String("secondary", "", "peer master address e.g. 192.168.1.20:50052 (leave empty if standalone)")
	myAddr := flag.String("addr", "0.0.0.0:50051", "this master's own listen address e.g. 192.168.1.10:50051")
	flag.Parse()

	// Setup log file for Master - all logs will be written to master.log
	if err := os.MkdirAll("log_files", 0755); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: failed to create log_files directory: %v\n", err)
		os.Exit(1)
	}
	logFile, err := os.OpenFile(fmt.Sprintf("log_files/master_%s.log", strings.NewReplacer(":", "-", ".", "-").Replace(*myAddr)), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: failed to open log file: %v\n", err)
		os.Exit(1)
	}
	defer logFile.Close()

	// Create custom logger with prefix "MASTER: " and timestamp
	masterLogger := log.New(logFile, fmt.Sprintf("MASTER(%s): ", *myAddr), log.LstdFlags|log.Lshortfile)

	// Write to both file and stderr so startup errors are always visible
	log.SetOutput(io.MultiWriter(os.Stderr, logFile))

	// Start listening — use the -addr flag so secondary can bind its own port
	lis, err := net.Listen("tcp", *myAddr)
	if err != nil {
		log.Fatalf("FATAL: Failed to listen on %s: %v", *myAddr, err)
	}
	fmt.Printf("Master listening on %s\n", *myAddr)

	// Create gRPC server instance
	s := grpc.NewServer()

	// Open WAL file for write-ahead logging
	// In standby mode, we open read-only initially, but for simplicity we keep it append
	// (Standby won't write unless promoted, but needs to read)
	// Actually, if we are on the same filesystem, only one writer allowed usually if using lock
	// But append mode is generally safe for single writer. Standby should probably just READ.
	// For now, let's open it same way.
	walFile, err := os.OpenFile("master.wal", os.O_CREATE|os.O_RDWR|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open WAL file: %v", err)
	}
	defer walFile.Close()

	// Initialize the MasterServer with empty maps
	server := &MasterServer{
		fileInfo:        make(map[int64]map[string]map[int32]*dfspb.StripeMetadata),
		clientIDs:       make(map[int64][]string),
		fileSizes:       make(map[int64]map[string]int64),
		chunkStatus:     make(map[string]string),
		chunkServers:    config.GetChunkServers(),
		servers:         make(map[string]*ServerInfo),
		clientFolders:   make(map[int64]map[string]bool),
		fileUploadTimes: make(map[int64]map[string]int64),
		clientUsernames: make(map[int64]string),
		logger:          masterLogger,
		walFile:         walFile,
		walWriter:       bufio.NewWriter(walFile),
		// --- failover fields ---
		peerAddr:      *peerAddr,
		secondaryAddr: *peerAddr, // will be used for WAL replication when primary
		myAddr:        *myAddr,
		isPrimary:     true, // assume primary; trySyncStateFromPeer may demote us
		generation:    1,    // starting generation; LoadCheckpoint may overwrite
	}

	// Restore from checkpoint first (if exists)
	if err := server.LoadCheckpoint("master.checkpoint"); err != nil {
		log.Fatalf("Checkpoint loading failed: %v", err)
	}

	// Then replay WAL entries after checkpoint
	if err := server.RecoverFromWAL("master.wal"); err != nil {
		log.Fatalf("WAL recovery failed: %v", err)
	}

	// --- STATE SYNC: if a peer is configured, check whether the peer
	// promoted itself while we were down and pull its full state if so.
	// trySyncStateFromPeer will set isPrimary=false if the peer is the active master. ---
	if *peerAddr != "" {
		if err := trySyncStateFromPeer(server, *peerAddr, masterLogger); err != nil {
			masterLogger.Printf("State sync from peer failed (will use local state as primary): %v", err)
			// Peer unreachable → we become primary
			server.isPrimary = true
		}
	}

	// Register MasterServer to handle client/chunkserver gRPC requests
	dfspb.RegisterMasterServerServer(s, server)

	// --- NEW: register SecondaryMasterServer so this node can also act as standby ---
	secondary := NewSecondaryMaster(server)
	dfspb.RegisterSecondaryMasterServerServer(s, secondary)

	// Start background goroutine for periodic checkpointing (every 5 minutes)
	go server.PeriodicCheckpoint(5, "master.checkpoint", "master.wal")

	// --- Start heartbeats or watchdog based on actual role ---
	if *peerAddr != "" {
		if server.isPrimary {
			go server.SendHeartbeatsToSecondary(*peerAddr)
			masterLogger.Printf("Primary mode: will send heartbeats to peer at %s", *peerAddr)
			fmt.Fprintf(os.Stderr, "\n╔══════════════════════════════════════════════╗\n")
			fmt.Fprintf(os.Stderr,   "║  ✅  ACTIVE PRIMARY MASTER: %-16s  ║\n", *myAddr)
			fmt.Fprintf(os.Stderr,   "║     Standby peer: %-26s  ║\n", *peerAddr)
			fmt.Fprintf(os.Stderr,   "╚══════════════════════════════════════════════╝\n\n")
		} else {
			// We synced state from the active master — run as standby
			go secondary.WatchdogLoop(10) // 10 second timeout
			masterLogger.Printf("Standby mode: watchdog started (will promote if master silent for 10s)")
			fmt.Fprintf(os.Stderr, "\n╔══════════════════════════════════════════════╗\n")
			fmt.Fprintf(os.Stderr,   "║  ⏳  STANDBY MASTER: %-23s  ║\n", *myAddr)
			fmt.Fprintf(os.Stderr,   "║     Watching primary: %-24s  ║\n", *peerAddr)
			fmt.Fprintf(os.Stderr,   "╚══════════════════════════════════════════════╝\n\n")
		}
	} else {
		// No peer configured — standalone primary
		masterLogger.Printf("Standalone mode: no peer configured")
		fmt.Fprintf(os.Stderr, "\n╔══════════════════════════════════════════════╗\n")
		fmt.Fprintf(os.Stderr,   "║  ✅  STANDALONE PRIMARY: %-20s  ║\n", *myAddr)
		fmt.Fprintf(os.Stderr,   "║     (no failover peer configured)             ║\n")
		fmt.Fprintf(os.Stderr,   "╚══════════════════════════════════════════════╝\n\n")
	}

	// Start periodic status printer — prints current role to terminal every 5 seconds
	go startStatusPrinter(server, secondary, *myAddr, *peerAddr)

	// Start background goroutine for dead chunk-server detection
	go func() {
		for {
			time.Sleep(10 * time.Second)
			server.serversMu.Lock()
			for addr, info := range server.servers {
				if time.Since(info.LastHeartbeat) > 20*time.Second {
					if info.Alive {
						info.Alive = false
						server.logger.Printf("DEAD SERVER DETECTED: %s", addr)
					}
				}
			}
			server.serversMu.Unlock()
		}
	}()

	log.Printf("Master running on %s — Logs to master.log", *myAddr)

	// Start serving - this blocks until server shuts down
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

// startStatusPrinter prints the current node role to stderr every 5 seconds.
// This makes it impossible to miss which node is the active primary at any point.
func startStatusPrinter(server *MasterServer, secondary *SecondaryMaster, myAddr, peerAddr string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		server.mu.Lock()
		isPrimary := server.isPrimary
		gen := server.generation
		walSeq := server.walSeq
		server.mu.Unlock()

		if isPrimary {
			fmt.Fprintf(os.Stderr, "[STATUS] %s → ✅ ACTIVE PRIMARY  (gen=%d, wal_seq=%d)\n",
				myAddr, gen, walSeq)
		} else {
			// Show how long since last heartbeat from primary
			secondary.mu.Lock()
			lastHB := secondary.lastHeartbeat
			primAddr := secondary.primaryAddr
			secondary.mu.Unlock()

			if primAddr == "" {
				primAddr = peerAddr
			}
			elapsed := time.Since(lastHB).Round(time.Second)
			fmt.Fprintf(os.Stderr, "[STATUS] %s → ⏳ STANDBY  (primary: %s, last heartbeat: %s ago)\n",
				myAddr, primAddr, elapsed)
		}
	}
}

// SendHeartbeatsToSecondary periodically pings the secondary master.
// Secondary uses these to detect primary failure and trigger auto-promotion.
func (m *MasterServer) SendHeartbeatsToSecondary(secondaryAddr string) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		conn, err := grpc.NewClient(secondaryAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			m.logger.Printf("Heartbeat to secondary: connect failed: %v", err)
			continue
		}

		client := dfspb.NewSecondaryMasterServerClient(conn)
		// Use a 2-second timeout so a network hiccup never blocks this goroutine
		// longer than 2s. Without a timeout, context.Background() hangs indefinitely
		// on packet loss, making the secondary falsely promote itself after 10 seconds.
		hbCtx, hbCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err = client.SendMasterHeartbeat(hbCtx, &dfspb.MasterHeartbeatRequest{
			PrimaryAddr:     m.myAddr,
			LastWalSequence: m.walSeq,
		})
		hbCancel()
		conn.Close()

		if err != nil {
			m.logger.Printf("Heartbeat to secondary failed: %v", err)
			log.Printf("WARNING: Heartbeat to secondary at %s failed: %v", secondaryAddr, err)
		} else {
			m.logger.Printf("Heartbeat sent to secondary at %s (wal_seq=%d)", secondaryAddr, m.walSeq)
		}
	}
}

// trySyncStateFromPeer checks whether the configured secondary peer has promoted
// itself to primary while *this* node was down.  If it has, we pull the full
// state checkpoint from the peer and replace our local (stale) in-memory state.
//
// This is the key fix for the "returning primary has stale metadata" problem:
//
//	Primary A dies.  Secondary B promotes (generation++).
//	A restarts, calls trySyncStateFromPeer(B).
//	B says IsPrimary:true → A pulls B's state (has C, D, and all deletes).
//	A then starts serving as primary with correct metadata.
func trySyncStateFromPeer(server *MasterServer, peerAddr string, logger *log.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(peerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("cannot connect to peer %s: %v", peerAddr, err)
	}
	defer conn.Close()

	// Ask the peer whether it is currently acting as primary
	masterClient := dfspb.NewMasterServerClient(conn)
	activeResp, err := masterClient.GetActiveMaster(ctx, &dfspb.GetActiveMasterRequest{})
	if err != nil {
		return fmt.Errorf("GetActiveMaster from peer %s failed: %v", peerAddr, err)
	}

	if !activeResp.IsPrimary {
		// Peer is still in standby — we are the rightful primary, use local state
		logger.Printf("Peer %s is not primary (isPrimary=false) — keeping local state (gen=%d)",
			peerAddr, server.generation)
		server.isPrimary = true
		return nil
	}

	// Peer has promoted itself (or was always primary).  Pull its full state
	// and demote ourselves to standby so there is only one active master.
	logger.Printf("Peer %s is the active primary — pulling full state sync (we will be standby)...", peerAddr)

	syncCtx, syncCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer syncCancel()

	secClient := dfspb.NewSecondaryMasterServerClient(conn)
	syncResp, err := secClient.RequestStateSync(syncCtx, &dfspb.GetActiveMasterRequest{})
	if err != nil {
		return fmt.Errorf("RequestStateSync from peer %s failed: %v", peerAddr, err)
	}

	// Decode the checkpoint the peer sent us
	var checkpoint Checkpoint
	if err := json.Unmarshal(syncResp.StateData, &checkpoint); err != nil {
		return fmt.Errorf("failed to decode checkpoint from peer: %v", err)
	}

	logger.Printf("Received state from peer: gen=%d, wal_seq=%d, clients=%d, chunks=%d",
		checkpoint.Generation, checkpoint.WALSeq,
		len(checkpoint.ClientIDs), len(checkpoint.ChunkStatus))

	// Apply peer state into our server (same path as LoadCheckpoint)
	server.mu.Lock()
	server.generation = checkpoint.Generation
	server.walSeq = checkpoint.WALSeq
	server.clientIDs = checkpoint.ClientIDs
	server.fileSizes = checkpoint.FileSizes
	server.chunkStatus = checkpoint.ChunkStatus
	if checkpoint.ClientFolders != nil {
		server.clientFolders = checkpoint.ClientFolders
	}
	if checkpoint.FileUploadTimes != nil {
		server.fileUploadTimes = checkpoint.FileUploadTimes
	}
	if checkpoint.ClientUsernames != nil {
		server.clientUsernames = checkpoint.ClientUsernames
	}

	// Rebuild fileInfo from checkpoint
	server.fileInfo = make(map[int64]map[string]map[int32]*dfspb.StripeMetadata)
	for clientID, filesJSON := range checkpoint.FileInfo {
		server.fileInfo[clientID] = make(map[string]map[int32]*dfspb.StripeMetadata)
		for filename, stripesJSON := range filesJSON {
			server.fileInfo[clientID][filename] = make(map[int32]*dfspb.StripeMetadata)
			for stripeNum, sj := range stripesJSON {
				server.fileInfo[clientID][filename][stripeNum] = &dfspb.StripeMetadata{
					StripeNum: sj.StripeNum,
					ChunkIds:  sj.ChunkIds,
					Servers:   sj.Servers,
				}
			}
		}
	}
	server.mu.Unlock()

	// Demote ourselves: peer is the active master, we are standby
	server.isPrimary = false

	// Persist this synced state locally so we survive our own crash
	if err := server.CreateCheckpoint("master.checkpoint"); err != nil {
		logger.Printf("WARNING: failed to save synced checkpoint locally: %v", err)
	}

	logger.Printf("STATE SYNC COMPLETE: now at gen=%d wal_seq=%d (synced from %s)",
		server.generation, server.walSeq, peerAddr)
	return nil
}
