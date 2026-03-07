package main

import (
	"bufio"
	"dfs-project/dfspb"
	"dfs-project/pkg/config"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"
)

// main starts the master server and background health monitoring
func main() {
	port := flag.String("port", "50051", "port to listen on")
	mode := flag.String("mode", "active", "server mode: active or standby")
	primaryAddr := flag.String("primary", "127.0.0.1:50051", "address of primary master to monitor (only for standby)")
	secondaryAddr := flag.String("secondary", "", "address of secondary master to sync metadata to (only for active)")
	flag.Parse()

	// Setup log file for Master - all logs will be written to master.log
	logFile, err := os.OpenFile(fmt.Sprintf("log_files/master_%s.log", *port), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logFile.Close()

	// Create custom logger with prefix "MASTER: " and timestamp
	masterLogger := log.New(logFile, fmt.Sprintf("MASTER(%s): ", *port), log.LstdFlags|log.Lshortfile)

	// Replace default log with file logger (for fatal errors)
	log.SetOutput(logFile)

	// Start listening on configured port
	lis, err := net.Listen("tcp", "0.0.0.0:"+*port)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

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
		fileInfo:        make(map[string]map[string]map[int32]*dfspb.StripeMetadata),
		clientIDs:       make(map[string][]string),
		fileSizes:       make(map[string]map[string]int64),
		chunkStatus:     make(map[string]string),           // Empty chunk status map
		chunkServers:    make([]string, 0),                 // Start empty, discover via heartbeats
		servers:         make(map[string]*ServerInfo),      // Empty server health map
		logger:          masterLogger,                      // Custom logger for file output
		walFile:         walFile,                           // WAL file handle
		walWriter:       bufio.NewWriter(walFile),          // Buffered WAL writer
		clientFolders:   make(map[string]map[string]bool),  // Folder hierarchy support
		fileUploadTimes: make(map[string]map[string]int64), // Upload timestamps
		IsStandby:       *mode == "standby",                // Set mode
		listenAddr:      "0.0.0.0:" + *port,                // Own listen address (for .master_addr update on promotion)
		userPasswords:   make(map[string]string),
	}

	// Restore from checkpoint first (if exists)
	if err := server.LoadCheckpoint("master.checkpoint"); err != nil {
		log.Fatalf("Checkpoint loading failed: %v", err)
	}

	// Then replay WAL entries after checkpoint
	if err := server.RecoverFromWAL("master.wal"); err != nil {
		log.Fatalf("WAL recovery failed: %v", err)
	}

	// Seed walOffset so the standby's incremental poller starts from the END of
	// the WAL we just read (not the beginning) to avoid replaying old entries.
	if fi, err := walFile.Stat(); err == nil {
		server.walOffset = fi.Size()
	}

	// If we are a standby, write our own address to .secondary_addr so clients
	// have a fallback address to discover us after the primary dies.
	if server.IsStandby {
		// Prefer the explicit env var passed by the Makefile (SECONDARY is the
		// full "IP:port" string), otherwise auto-detect the LAN IP.
		secondaryPublicAddr := os.Getenv("SECONDARY_MASTER_ADDR")
		if secondaryPublicAddr == "" {
			if ip, err := config.GetLocalIP(); err == nil {
				secondaryPublicAddr = ip + ":" + *port
			} else {
				secondaryPublicAddr = "127.0.0.1:" + *port
			}
		}
		if err := os.WriteFile(".secondary_addr", []byte(secondaryPublicAddr+"\n"), 0644); err != nil {
			log.Printf("Warning: could not write .secondary_addr: %v", err)
		} else {
			log.Printf("Standby: wrote .secondary_addr = %s", secondaryPublicAddr)
		}
	} else {
		// If we are active (Primary), write our LAN address to .master_addr so
		// chunkservers and clients can discover us without needing flags.
		// Use MASTER_ADDR env var (set by Makefile run-master-lan), else auto-detect.
		masterPublicAddr := os.Getenv("MASTER_ADDR")
		if masterPublicAddr == "" {
			if ip, err := config.GetLocalIP(); err == nil {
				masterPublicAddr = ip + ":" + *port
			} else {
				masterPublicAddr = "127.0.0.1:" + *port
			}
		}
		if err := os.WriteFile(".master_addr", []byte(masterPublicAddr+"\n"), 0644); err != nil {
			log.Printf("Warning: could not write .master_addr: %v", err)
		} else {
			log.Printf("Primary: wrote .master_addr = %s", masterPublicAddr)
		}
	}
	// Register our MasterServer to handle gRPC requests
	dfspb.RegisterMasterServerServer(s, server)

	// Start background goroutine for periodic checkpointing (every 5 minutes)
	go server.PeriodicCheckpoint(5, "master.checkpoint", "master.wal")

	// Start background goroutine for dead server detection
	// This runs continuously in the background checking for dead servers
	go func() {
		for {
			time.Sleep(10 * time.Second) // Check every 10 seconds

			server.serversMu.Lock()
			for addr, info := range server.servers {
				// If no heartbeat received in 20+ seconds, mark server as dead
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

	// Start monitoring Primary if we are Standby
	if server.IsStandby {
		go server.MonitorPrimary(*primaryAddr)
	} else {
		// If we are Active, start syncing our data to the Secondary
		go server.StartBackupSync(*secondaryAddr)
	}

	log.Printf("Master running on :%s (Mode: %s) – Logs to %s", *port, *mode, logFile.Name())

	// Start serving - this blocks until server shuts down
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
