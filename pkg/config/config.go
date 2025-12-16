package config

import "os"

// GetMasterAddr returns the master server address
// Defaults to localhost for local development, can be overridden with env var
func GetMasterAddr() string {
	addr := os.Getenv("MASTER_ADDR")
	if addr == "" {
		return "127.0.0.1:50051" // Default for local development
	}
	return addr
}

// GetChunkServers returns the list of chunk server addresses
// For Docker, use hostnames; for local, use localhost
func GetChunkServers() []string {
	// Check if running in Docker (environment variable set by docker-compose)
	if os.Getenv("DFS_ROLE") == "master" {
		// Docker mode: use container hostnames
		return []string{
			"chunkserver1:9001",
			"chunkserver2:9002",
			"chunkserver3:9003",
		}
	}
	
	// Local development mode: use localhost
	return []string{
		"127.0.0.1:9001",
		"127.0.0.1:9002",
		"127.0.0.1:9003",
	}
}

// GetMyAddr returns this chunk server's address for registration with master
// port: the port this chunk server is listening on
func GetMyAddr(port string) string {
	// If HOSTNAME is set (Docker), use it
	hostname := os.Getenv("HOSTNAME")
	if hostname != "" {
		return hostname + ":" + port
	}
	
	// Otherwise use localhost (local development)
	return "127.0.0.1:" + port
}
