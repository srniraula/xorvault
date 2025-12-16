package main

import (
	"bufio"
	"dfs-project/dfspb"
	"log"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"
)

// getLocalIP returns the non-loopback local IP of the host
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		// Check if it's an IP address (not loopback)
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

// main starts the master server and background health monitoring
func main() {
	// Setup log file for Master - all logs will be written to master.log
	logFile, err := os.OpenFile("master.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open master.log: %v", err)
	}
	defer logFile.Close()

	// Create custom logger with prefix "MASTER: " and timestamp
	masterLogger := log.New(logFile, "MASTER: ", log.LstdFlags|log.Lshortfile)

	// Replace default log with file logger (for fatal errors)
	log.SetOutput(logFile)

	// Get local IP address
	localIP := getLocalIP()
	if localIP == "" {
		log.Fatalf("Failed to determine local IP address")
	}

	masterLogger.Printf("Master server IP: %s", localIP)

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

	// Initialize the MasterServer with empty maps
	// Chunk servers will register themselves via heartbeats
	server := &MasterServer{
		fileInfo:     make(map[string]map[int32]*dfspb.StripeMetadata),
		clientIDs:    make(map[int64][]string),
		fileSizes:    make(map[string]int64),
		chunkStatus:  make(map[string]string),
		chunkServers: []string{}, // Empty - will be populated by heartbeats
		servers:      make(map[string]*ServerInfo),
		logger:       masterLogger,
		walFile:      walFile,
		walWriter:    bufio.NewWriter(walFile),
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
	go func() {
		for {
			time.Sleep(10 * time.Second)

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

	log.Printf("Master running on %s:50051 – Logs to master.log", localIP)
	masterLogger.Printf("============================================")
	masterLogger.Printf("Master Server Started")
	masterLogger.Printf("Address: %s:50051", localIP)
	masterLogger.Printf("Waiting for chunk servers to register...")
	masterLogger.Printf("============================================")

	// Start serving - this blocks until server shuts down
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
