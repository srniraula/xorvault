package main

import (
	"dfs-project/dfspb"
	"flag"
	"log"
	"net"
	"os"

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

func main() {
	port := flag.String("port", "9001", "server port")
	storage := flag.String("storage", "chunks", "storage directory")
	masterAddr := flag.String("master", "127.0.0.1:50051", "master server address (IP:PORT)")
	flag.Parse()

	// Setup logging
	logFile, err := os.OpenFile("chunkserver.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open chunkserver.log: %v", err)
	}
	defer logFile.Close()

	chunkLogger := log.New(logFile, "CHUNKSERVER: ", log.LstdFlags|log.Lshortfile)
	log.SetOutput(logFile)

	// Get local IP address
	localIP := getLocalIP()
	if localIP == "" {
		log.Fatalf("Failed to determine local IP address")
	}

	chunkLogger.Printf("Chunk server IP: %s", localIP)
	chunkLogger.Printf("Master server: %s", *masterAddr)

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

	// Perform inventory check on startup (with master address and local IP)
	go PerformInventoryCheck(server, localIP, *port, *masterAddr, chunkLogger)

	// Start heartbeat goroutine (with master address and local IP)
	go SendHeartbeats(localIP, *port, *masterAddr, chunkLogger)

	chunkLogger.Printf("============================================")
	chunkLogger.Printf("ChunkServer Started")
	chunkLogger.Printf("Address: %s:%s", localIP, *port)
	chunkLogger.Printf("Storage: %s", *storage)
	chunkLogger.Printf("Master: %s", *masterAddr)
	chunkLogger.Printf("============================================")

	log.Printf("ChunkServer running on %s:%s (storage: %s)", localIP, *port, *storage)

	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
