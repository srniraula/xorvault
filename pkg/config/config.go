package config

import (
	"errors"
	"net"
	"os"
	"strings"
	"time"
)

// ── Chunk / Stripe sizing ──────────────────────────────────────────────────
// ChunkSize is the size of a single data chunk in bytes.
// This is the ONLY place you need to change it — all components import it.
// Current: 1 MB. To use 2 MB chunks: change to 2 * 1024 * 1024.
const ChunkSize = 1 * 1024 * 1024

// StripeSize is the amount of user data stored per stripe (two data chunks).
// Always derived from ChunkSize — do NOT set this independently.
const StripeSize = 2 * ChunkSize

// ── Per-chunk RPC timeouts ─────────────────────────────────────────────────
// ChunkWriteTimeout is the gRPC deadline for a single WriteChunk RPC.
// Covers: TCP round-trip + chunkserver disk write + ack.
// To adjust for slower networks or higher load, change only this constant.
const ChunkWriteTimeout = 30 * time.Second

// ChunkReadTimeout is the gRPC deadline for a single ReadChunk RPC.
// Covers: TCP round-trip + chunkserver disk read + response transfer.
const ChunkReadTimeout = 30 * time.Second


// GetMasterAddr returns the master server address
// Defaults to localhost for local development, can be overridden with env var
func GetMasterAddr() string {
	addr := os.Getenv("MASTER_ADDR")
	if addr == "" {
		if data, err := os.ReadFile(".master_addr"); err == nil {
			if fileAddr := strings.TrimSpace(string(data)); fileAddr != "" {
				addr = fileAddr
			}
		}
	}
	if addr == "" {
		// Try to detect a non-loopback IPv4 address on the device so the
		// master can be reached from other devices on the LAN. Fall back to
		// localhost if none found.
		if ip, err := GetLocalIP(); err == nil {
			return ip + ":50051"
		}
		return "127.0.0.1:50051" // Default for local development
	}
	return addr
}

// GetSecondaryMasterAddr returns the secondary (standby) master address.
// Priority order:
//  1. SECONDARY_MASTER_ADDR environment variable
//  2. .secondary_master_addr file in the working directory
//  3. empty string if neither is set
func GetSecondaryMasterAddr() string {
	if addr := os.Getenv("SECONDARY_MASTER_ADDR"); addr != "" {
		return strings.TrimSpace(addr)
	}
	if data, err := os.ReadFile(".secondary_master_addr"); err == nil {
		if addr := strings.TrimSpace(string(data)); addr != "" {
			return addr
		}
	}
	return ""
}

// GetMasterAddrs returns all known master addresses (primary + secondary).
// Address resolution order:
//  1. MASTER_ADDRS env var (comma-separated list overrides everything)
//  2. Primary from MASTER_ADDR / .master_addr file,
//     secondary from SECONDARY_MASTER_ADDR / .secondary_master_addr file
func GetMasterAddrs() []string {
	// Explicit list takes priority
	if addrs := os.Getenv("MASTER_ADDRS"); addrs != "" {
		parts := strings.Split(addrs, ",")
		var result []string
		for _, p := range parts {
			if a := strings.TrimSpace(p); a != "" {
				result = append(result, a)
			}
		}
		if len(result) > 0 { 
			return result
		}
	}

	// Build list from individual primary + secondary sources
	var result []string
	result = append(result, GetMasterAddr())
	if sec := GetSecondaryMasterAddr(); sec != "" {
		result = append(result, sec)
	}
	return result
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

	// Local development/LAN mode: start empty.
	// Chunkservers will register themselves dynamically when they
	// send their first heartbeat to the master.
	return []string{}
}

// GetMyAddr returns this chunk server's address for registration with master
// port: the port this chunk server is listening on
func GetMyAddr(port string) string {
	// If HOSTNAME is set (Docker), use it
	hostname := os.Getenv("HOSTNAME")
	if hostname != "" {
		return hostname + ":" + port
	}

	// Otherwise try to pick a non-loopback device IP so other machines on
	// the LAN can reach this server. Fall back to localhost if detection
	// fails.
	if ip, err := GetLocalIP(); err == nil {
		return ip + ":" + port
	}
	return "127.0.0.1:" + port
}

// GetLocalIP returns the first non-loopback IPv4 address found on the
// machine, prioritizing the bridge network interface (enp0s9).
func GetLocalIP() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	// Priority 1: Look for virtualbox host-only network interface (enp0s8) first
	for _, iface := range ifaces {
		if iface.Name == "enp0s8" && iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagLoopback == 0 {
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil && !ipnet.IP.IsLoopback() {
					return ipnet.IP.String(), nil
				}
			}
		}
	}

	// Priority 2: Fall back to any other non-loopback interface
	for _, iface := range ifaces {
		// skip down, loopback interfaces, or NAT interface
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil && !ipnet.IP.IsLoopback() {
				ip := ipnet.IP.String()
				// Skip NAT interface (10.0.2.x)
				if !strings.HasPrefix(ip, "10.0.2.") {
					return ip, nil
				}
			}
		}
	}

	return "", errors.New("no suitable IPv4 address found")
}
