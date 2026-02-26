// Package config reads cluster topology from cluster.conf and environment variables.
//
// Priority order (highest to lowest):
//  1. Environment variable  (MASTER_ADDR, etc.)
//  2. cluster.conf file      (recommended for LAN setups)
//  3. .master_addr file      (dynamic — written by primary on startup, updated by secondary on promotion)
//  4. Auto-detected LAN IP   (fallback for single-machine dev)
package config

import (
	"bufio"
	"errors"
	"net"
	"os"
	"strings"
)

// clusterConf holds the parsed cluster.conf values.
type clusterConf struct {
	PrimaryMasterIP     string
	PrimaryMasterPort   string
	SecondaryMasterIP   string
	SecondaryMasterPort string
	ChunkServers        []chunkSrvEntry
	WebAPIPort          string
	FrontendPort        string
	SecondarySSHUser    string
}

type chunkSrvEntry struct {
	IP   string
	Port string
}

// loadClusterConf parses cluster.conf from the current directory (or via
// CLUSTER_CONF env var) and returns a populated clusterConf struct.
// Missing or malformed files are tolerated — the struct simply stays zero.
func loadClusterConf() clusterConf {
	path := os.Getenv("CLUSTER_CONF")
	if path == "" {
		path = "cluster.conf"
	}

	f, err := os.Open(path)
	if err != nil {
		return clusterConf{} // file not found — that's OK
	}
	defer f.Close()

	conf := clusterConf{}
	scanner := bufio.NewScanner(f)
	kv := map[string]string{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			kv[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	conf.PrimaryMasterIP = kv["PRIMARY_MASTER_IP"]
	conf.PrimaryMasterPort = kv["PRIMARY_MASTER_PORT"]
	if conf.PrimaryMasterPort == "" {
		conf.PrimaryMasterPort = "50051"
	}

	conf.SecondaryMasterIP = kv["SECONDARY_MASTER_IP"]
	conf.SecondaryMasterPort = kv["SECONDARY_MASTER_PORT"]
	if conf.SecondaryMasterPort == "" {
		conf.SecondaryMasterPort = "50052"
	}

	conf.WebAPIPort = kv["WEB_API_PORT"]
	if conf.WebAPIPort == "" {
		conf.WebAPIPort = "8080"
	}

	conf.FrontendPort = kv["FRONTEND_PORT"]
	if conf.FrontendPort == "" {
		conf.FrontendPort = "5173"
	}

	conf.SecondarySSHUser = kv["SECONDARY_SSH_USER"]

	// Parse chunk servers 1-9
	for i := 1; i <= 9; i++ {
		ipKey := strings.Replace("CHUNK_SERVER_N_IP", "N", string(rune('0'+i)), 1)
		portKey := strings.Replace("CHUNK_SERVER_N_PORT", "N", string(rune('0'+i)), 1)
		ip := kv[ipKey]
		port := kv[portKey]
		if ip != "" && port != "" {
			conf.ChunkServers = append(conf.ChunkServers, chunkSrvEntry{IP: ip, Port: port})
		}
	}

	return conf
}

// GetMasterAddr returns the active primary master address.
// Resolution order:
//  1. MASTER_ADDR env
//  2. PRIMARY_MASTER_IP:PORT from cluster.conf
//  3. .master_addr file (dynamic, updated on failover)
//  4. Auto-detected LAN IP:50051
func GetMasterAddr() string {
	if addr := os.Getenv("MASTER_ADDR"); addr != "" {
		return addr
	}

	conf := loadClusterConf()
	if conf.PrimaryMasterIP != "" {
		return conf.PrimaryMasterIP + ":" + conf.PrimaryMasterPort
	}

	// Fall back to dynamic .master_addr written by master on startup
	if data, err := os.ReadFile(".master_addr"); err == nil {
		if addr := strings.TrimSpace(string(data)); addr != "" {
			return addr
		}
	}

	if ip, err := GetLocalIP(); err == nil {
		return ip + ":50051"
	}
	return "127.0.0.1:50051"
}

// GetSecondaryMasterAddr returns the secondary (standby) master address.
// Returns "" if no secondary is configured.
func GetSecondaryMasterAddr() string {
	if addr := os.Getenv("SECONDARY_MASTER_ADDR"); addr != "" {
		return addr
	}
	conf := loadClusterConf()
	if conf.SecondaryMasterIP != "" {
		return conf.SecondaryMasterIP + ":" + conf.SecondaryMasterPort
	}
	if data, err := os.ReadFile(".secondary_addr"); err == nil {
		if addr := strings.TrimSpace(string(data)); addr != "" {
			return addr
		}
	}
	return ""
}

// GetChunkServers returns the list of chunk server addresses from cluster.conf,
// falling back to localhost defaults for single-machine development.
func GetChunkServers() []string {
	// Docker mode
	if os.Getenv("DFS_ROLE") == "master" {
		return []string{
			"chunkserver1:9001",
			"chunkserver2:9002",
			"chunkserver3:9003",
		}
	}

	conf := loadClusterConf()
	if len(conf.ChunkServers) > 0 {
		addrs := make([]string, len(conf.ChunkServers))
		for i, cs := range conf.ChunkServers {
			addrs[i] = cs.IP + ":" + cs.Port
		}
		return addrs
	}

	// Local fallback
	return []string{
		"127.0.0.1:9001",
		"127.0.0.1:9002",
		"127.0.0.1:9003",
	}
}

// GetMyAddr returns this chunk server's LAN address for heartbeat registration.
// Resolution order:
//  1. CHUNKSERVER_ADDR env (fully explicit, e.g. "192.168.1.102:9001")
//  2. Auto-detected LAN IP + given port
func GetMyAddr(port string) string {
	if addr := os.Getenv("CHUNKSERVER_ADDR"); addr != "" {
		return addr
	}
	if hostname := os.Getenv("HOSTNAME"); hostname != "" && os.Getenv("DFS_ROLE") != "" {
		return hostname + ":" + port
	}
	if ip, err := GetLocalIP(); err == nil {
		return ip + ":" + port
	}
	return "127.0.0.1:" + port
}

// GetWebAPIPort returns the web API listen port.
func GetWebAPIPort() string {
	if p := os.Getenv("WEB_API_PORT"); p != "" {
		return p
	}
	conf := loadClusterConf()
	if conf.WebAPIPort != "" {
		return conf.WebAPIPort
	}
	return "8080"
}

// GetLocalIP returns the first non-loopback IPv4 address found on the machine.
func GetLocalIP() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue
			}
			return ip.String(), nil
		}
	}
	return "", errors.New("no non-loopback IPv4 address found")
}
