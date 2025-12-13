package main

import (
	"flag"
	"log"
	"net"
	"os"
	"dfs-project/dfspb"
	"google.golang.org/grpc"
)


func main() {
	port := flag.String("port", "9001", "server port")
	storage := flag.String("storage", "chunks", "storage directory")
	flag.Parse()

	// Setup logging
	logFile, err := os.OpenFile("chunkserver.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open chunkserver.log: %v", err)
	}
	defer logFile.Close()

	chunkLogger := log.New(logFile, "CHUNKSERVER: ", log.LstdFlags|log.Lshortfile)
	log.SetOutput(logFile)

	// Create storage directory
	os.MkdirAll(*storage, 0755)

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

	// Perform inventory check on startup
	go PerformInventoryCheck(server, *port, chunkLogger)

	// Start heartbeat goroutine
	go SendHeartbeats(*port, chunkLogger)

	log.Printf("ChunkServer running on 0.0.0.0:%s (storage: %s)", *port, *storage)

	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
