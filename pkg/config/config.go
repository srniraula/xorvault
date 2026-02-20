package config

import (
	"errors"
	"net"
	"os"
	"strings"
)

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
	
	// Priority 1: Look for bridge network interface (enp0s9) first
	for _, iface := range ifaces {
		if iface.Name == "enp0s9" && iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagLoopback == 0 {
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
