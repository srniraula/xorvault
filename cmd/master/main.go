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
	"flag"
	"fmt"
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
	// --- NEW: command-line flags for failover ---
	secondaryAddr := flag.String("secondary", "", "secondary master address e.g. 192.168.1.20:50052 (leave empty if none)")
	myAddr := flag.String("addr", "0.0.0.0:50051", "this master's own listen address e.g. 192.168.1.10:50051")
	flag.Parse()

	// Setup log file for Master - all logs will be written to master.log
	logFile, err := os.OpenFile(fmt.Sprintf("log_files/master_%s.log", strings.NewReplacer(":", "-", ".", "-").Replace(*myAddr)), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logFile.Close()

	// Create custom logger with prefix "MASTER: " and timestamp
	masterLogger := log.New(logFile, fmt.Sprintf("MASTER(%s): ", *myAddr), log.LstdFlags|log.Lshortfile)

	// Replace default log with file logger (for fatal errors)
	log.SetOutput(logFile)

	// Start listening — use the -addr flag so secondary can bind its own port
	lis, err := net.Listen("tcp", *myAddr)
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
		fileInfo:     make(map[int64]map[string]map[int32]*dfspb.StripeMetadata),
		clientIDs:    make(map[int64][]string),
		fileSizes:    make(map[int64]map[string]int64),
		chunkStatus:  make(map[string]string),
		chunkServers: config.GetChunkServers(),
		servers:      make(map[string]*ServerInfo),
		logger:       masterLogger,
		walFile:      walFile,
		walWriter:    bufio.NewWriter(walFile),
		// --- NEW failover fields ---
		secondaryAddr: *secondaryAddr,
		myAddr:        *myAddr,
		isPrimary:     (*secondaryAddr != ""), // If we have a secondary to talk to, we are primary
	}

	// Restore from checkpoint first (if exists)
	if err := server.LoadCheckpoint("master.checkpoint"); err != nil {
		log.Fatalf("Checkpoint loading failed: %v", err)
	}

	// Then replay WAL entries after checkpoint
	if err := server.RecoverFromWAL("master.wal"); err != nil {
		log.Fatalf("WAL recovery failed: %v", err)
	}

	// Register MasterServer to handle client/chunkserver gRPC requests
	dfspb.RegisterMasterServerServer(s, server)

	// --- NEW: register SecondaryMasterServer so this node can also act as standby ---
	secondary := NewSecondaryMaster(server)
	dfspb.RegisterSecondaryMasterServerServer(s, secondary)

	// Start background goroutine for periodic checkpointing (every 5 minutes)
	go server.PeriodicCheckpoint(5, "master.checkpoint", "master.wal")

	// --- NEW: if a secondary is configured, send it heartbeats ---
	if *secondaryAddr != "" {
		go server.SendHeartbeatsToSecondary(*secondaryAddr)
		masterLogger.Printf("Primary mode: will send heartbeats to secondary at %s", *secondaryAddr)
	} else {
		// No secondary configured → we might be the secondary ourselves.
		// Start the watchdog: if primary heartbeats stop arriving, promote ourselves.
		go secondary.WatchdogLoop(10) // 10 second timeout
		masterLogger.Printf("Standby mode: watchdog started (will promote if primary silent for 10s)")
	}

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
		_, err = client.SendMasterHeartbeat(context.Background(), &dfspb.MasterHeartbeatRequest{
			PrimaryAddr:     m.myAddr,
			LastWalSequence: m.walSeq,
		})
		conn.Close()

		if err != nil {
			m.logger.Printf("Heartbeat to secondary failed: %v", err)
			log.Printf("WARNING: Heartbeat to secondary at %s failed: %v", secondaryAddr, err)
		} else {
			m.logger.Printf("Heartbeat sent to secondary at %s (wal_seq=%d)", secondaryAddr, m.walSeq)
		}
	}
}
