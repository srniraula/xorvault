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
	peerAddr := flag.String("secondary", "", "peer master address e.g. 192.168.1.20:50052 (leave empty if standalone)")
	myAddr := flag.String("addr", "0.0.0.0:50051", "this master's own listen address e.g. 192.168.1.10:50051")
	role := flag.String("role", "primary", "startup role: 'primary' or 'secondary'. Secondary always starts in standby.")
	flag.Parse()

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

	masterLogger := log.New(logFile, fmt.Sprintf("MASTER(%s): ", *myAddr), log.LstdFlags|log.Lshortfile)

	log.SetOutput(io.MultiWriter(os.Stderr, logFile))

	// Start listening — use the -addr flag so secondary can bind its own port
	lis, err := net.Listen("tcp", *myAddr)
	if err != nil {
		log.Fatalf("FATAL: Failed to listen on %s: %v", *myAddr, err)
	}
	fmt.Printf("Master listening on %s\n", *myAddr)

	s := grpc.NewServer()

	walFile, err := os.OpenFile("master.wal", os.O_CREATE|os.O_RDWR|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open WAL file: %v", err)
	}
	defer walFile.Close()

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
		peerAddr:        *peerAddr,
		secondaryAddr:   *peerAddr,
		myAddr:          *myAddr,
		isPrimary:       *role != "secondary",
		generation:      1,
	}

	if err := server.LoadCheckpoint("master.checkpoint"); err != nil {
		log.Fatalf("Checkpoint loading failed: %v", err)
	}

	if err := server.RecoverFromWAL("master.wal"); err != nil {
		log.Fatalf("WAL recovery failed: %v", err)
	}
	// SECONDARY: Always start in standby. Never contact the peer at startup to decide
	//            role — the WatchdogLoop will promote us only after 15s of silence + 3 probes.
	// PRIMARY:   Check if the peer promoted itself while we were down and pull its
	//            full state if so. If peer unreachable, use local WAL state and proceed.
	if *role == "secondary" {
		server.isPrimary = false
		masterLogger.Printf("Starting as SECONDARY (standby). Will promote only if primary silent for 15s + 3 probes.")
	} else if *peerAddr != "" {
		if err := trySyncStateFromPeer(server, *peerAddr, masterLogger); err != nil {
			masterLogger.Printf("State sync from peer failed (peer unreachable; using local WAL state as primary): %v", err)
			server.isPrimary = true
		}
	}

	dfspb.RegisterMasterServerServer(s, server)

	secondary := NewSecondaryMaster(server)
	dfspb.RegisterSecondaryMasterServerServer(s, secondary)

	go server.PeriodicCheckpoint(5, "master.checkpoint", "master.wal")

	if *peerAddr != "" {
		if server.isPrimary {
			go server.SendHeartbeatsToSecondary(*peerAddr)
			masterLogger.Printf("Primary mode: will send heartbeats to peer at %s", *peerAddr)
			fmt.Fprintf(os.Stderr, "\n╔══════════════════════════════════════════════╗\n")
			fmt.Fprintf(os.Stderr, "║  ✅  ACTIVE PRIMARY MASTER: %-16s  ║\n", *myAddr)
			fmt.Fprintf(os.Stderr, "║     Standby peer: %-26s  ║\n", *peerAddr)
			fmt.Fprintf(os.Stderr, "╚══════════════════════════════════════════════╝\n\n")
		} else {
			go secondary.WatchdogLoop(15)
			masterLogger.Printf("Standby mode: watchdog started (will promote if master silent for 15s + 3 probes)")
			fmt.Fprintf(os.Stderr, "\n╔══════════════════════════════════════════════╗\n")
			fmt.Fprintf(os.Stderr, "║  ⏳  STANDBY MASTER: %-23s  ║\n", *myAddr)
			fmt.Fprintf(os.Stderr, "║     Watching primary: %-24s  ║\n", *peerAddr)
			fmt.Fprintf(os.Stderr, "╚══════════════════════════════════════════════╝\n\n")
		}
	} else {
		masterLogger.Printf("Standalone mode: no peer configured")
		fmt.Fprintf(os.Stderr, "\n╔══════════════════════════════════════════════╗\n")
		fmt.Fprintf(os.Stderr, "║  ✅  STANDALONE PRIMARY: %-20s  ║\n", *myAddr)
		fmt.Fprintf(os.Stderr, "║     (no failover peer configured)             ║\n")
		fmt.Fprintf(os.Stderr, "╚══════════════════════════════════════════════╝\n\n")
	}

	go startStatusPrinter(server, secondary, *myAddr, *peerAddr)

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
//
// KEY DESIGN: We re-dial from scratch on every failure rather than reusing a
// stuck connection. When the peer was down and comes back up, gRPC's internal
// reconnect backoff on a shared connection can take 30-120s to recover — far
// longer than our 15s watchdog timeout on the secondary. By closing the broken
// connection and dialing fresh, we reach the recovered peer within 1-2s.
func (m *MasterServer) SendHeartbeatsToSecondary(secondaryAddr string) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var conn *grpc.ClientConn
	var client dfspb.SecondaryMasterServerClient

	dial := func() bool {
		if conn != nil {
			conn.Close()
			conn = nil
			client = nil
		}
		var err error
		conn, err = grpc.NewClient(secondaryAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			m.logger.Printf("Heartbeat: dial to secondary %s failed: %v", secondaryAddr, err)
			return false
		}
		client = dfspb.NewSecondaryMasterServerClient(conn)
		return true
	}

	dial()

	for range ticker.C {
		if client == nil {
			if !dial() {
				continue
			}
		}

		hbCtx, hbCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := client.SendMasterHeartbeat(hbCtx, &dfspb.MasterHeartbeatRequest{
			PrimaryAddr:     m.myAddr,
			LastWalSequence: m.walSeq,
		}, grpc.WaitForReady(true))
		hbCancel()

		if err != nil {
			m.logger.Printf("Heartbeat to secondary %s failed — will re-dial next tick: %v", secondaryAddr, err)
			conn.Close()
			conn = nil
			client = nil
		} else {
			m.logger.Printf("Heartbeat sent to secondary %s (wal_seq=%d)", secondaryAddr, m.walSeq)
		}
	}

	if conn != nil {
		conn.Close()
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

	masterClient := dfspb.NewMasterServerClient(conn)
	activeResp, err := masterClient.GetActiveMaster(ctx, &dfspb.GetActiveMasterRequest{})
	if err != nil {
		return fmt.Errorf("GetActiveMaster from peer %s failed: %v", peerAddr, err)
	}

	if !activeResp.IsPrimary {
		logger.Printf("Peer %s is not primary (isPrimary=false) — keeping local state (gen=%d)",
			peerAddr, server.generation)
		server.isPrimary = true
		return nil
	}

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

	server.isPrimary = false

	if err := server.CreateCheckpoint("master.checkpoint"); err != nil {
		logger.Printf("WARNING: failed to save synced checkpoint locally: %v", err)
	}

	logger.Printf("STATE SYNC COMPLETE: now at gen=%d wal_seq=%d (synced from %s)",
		server.generation, server.walSeq, peerAddr)
	return nil
}
