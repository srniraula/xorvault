package main

import (
	"bufio"
	"dfs-project/dfspb"
	"dfs-project/pkg/config"
	"log"
	"net"
	"os"
	"time"
	"google.golang.org/grpc"
)

// main starts the master server and background health monitoring
func main() {
	// Setup log file for Master - all logs will be written to master.log
	logFile, err := os.OpenFile("log_files/master.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open master.log: %v", err)
	}
	defer logFile.Close()

	// Create custom logger with prefix "MASTER: " and timestamp
	masterLogger := log.New(logFile, "MASTER: ", log.LstdFlags|log.Lshortfile)

	// Replace default log with file logger (for fatal errors)
	log.SetOutput(logFile)

	// Start listening on port 50051 for incoming gRPC requests
	lis, err := net.Listen("tcp", "0.0.0.0:50051")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	// Create gRPC server instance
	s := grpc.NewServer()

	// Open WAL file for write-ahead logging
	walFile, err := os.OpenFile("master.wal", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open WAL file: %v", err)
	}
	defer walFile.Close()

	// Initialize the MasterServer with empty maps and known chunk server addresses
	server := &MasterServer{
		fileInfo:     make(map[int64]map[string]map[int32]*dfspb.StripeMetadata),
		clientIDs:    make(map[int64][]string),
		fileSizes:    make(map[int64]map[string]int64),
		chunkStatus:  make(map[string]string),  // Empty chunk status map
		chunkServers: config.GetChunkServers(), // Get chunk servers from config (Docker-aware)
		servers:      make(map[string]*ServerInfo), // Empty server health map
		logger:       masterLogger,                 // Custom logger for file output
		walFile:      walFile,                      // WAL file handle
		walWriter:    bufio.NewWriter(walFile),     // Buffered WAL writer
	}

	// Restore from checkpoint first (if exists)
	if err := server.LoadCheckpoint("master.checkpoint"); err != nil {
		log.Fatalf("Checkpoint loading failed: %v", err)
	}

	// Then replay WAL entries after checkpoint
	if err := server.RecoverFromWAL("master.wal"); err != nil {
		log.Fatalf("WAL recovery failed: %v", err)
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

	log.Println("Master running on :50051 – Logs to master.log")

	// Start serving - this blocks until server shuts down
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
