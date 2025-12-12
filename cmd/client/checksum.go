package main

import (
	"fmt"
	"hash/crc32"
)

// calculateChecksum computes CRC32 checksum of data
// Returns hex-encoded string representation (8 characters)
// CRC32 is much faster and more memory efficient than SHA256 (4 bytes vs 32 bytes)
func calculateChecksum(data []byte) string {
	checksum := crc32.ChecksumIEEE(data)
	return fmt.Sprintf("%08x", checksum)
}
