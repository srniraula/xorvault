package main

import (
	"dfs-project/dfspb"
	"dfs-project/pkg/config"
	"flag"
	"log"
	"net"
	"os"

	"google.golang.org/grpc"
)

func main() {
	port := flag.String("port", "9001", "server port")
	storage := flag.String("storage", "chunks", "storage directory")
	master := flag.String("master", "", "primary master server address (host:port)")
	secondaryMaster := flag.String("secondary-master", "", "secondary master address for automatic failover (host:port, optional)")
	flag.Parse()

	// Setup logging
	logFile, err := os.OpenFile("log_files/chunkserver.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open chunkserver.log: %v", err)
	}
	defer logFile.Close()

	chunkLogger := log.New(logFile, "CHUNKSERVER: ", log.LstdFlags|log.Lshortfile)
	log.SetOutput(logFile)

	// Create storage directory
	os.MkdirAll(*storage, 0755)

	// Determine primary master address.
	// Prefer the explicit -master flag; fall back to config/env (MASTER_ADDR).
	primaryAddr := *master
	if primaryAddr == "" {
		primaryAddr = config.GetMasterAddr()
	}

	// Build the master tracker — knows both primary and optional secondary.
	tracker := NewMasterTracker(primaryAddr, *secondaryMaster)

	if *secondaryMaster != "" {
		chunkLogger.Printf("Master failover enabled: primary=%s, secondary=%s", primaryAddr, *secondaryMaster)
	} else {
		chunkLogger.Printf("No secondary master configured — failover disabled (primary=%s)", primaryAddr)
	}

	// Start gRPC server
	lis, err := net.Listen("tcp", "0.0.0.0:"+*port)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	s := grpc.NewServer()
	server := &ChunkServer{
		storagePath: *storage,
		logger:      chunkLogger,
	}
	dfspb.RegisterChunkServerServer(s, server)

	// Perform inventory check on startup — uses active master from tracker.
	go PerformInventoryCheck(server, *port, tracker, chunkLogger)

	// Start heartbeat goroutine — handles automatic failover via MasterTracker.
	go SendHeartbeats(*port, tracker, chunkLogger)

	log.Printf("ChunkServer running on 0.0.0.0:%s (storage: %s)", *port, *storage)

	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
