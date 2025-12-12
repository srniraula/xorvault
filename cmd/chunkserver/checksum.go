package main

import (
	"fmt"
	"hash/crc32"
)

// calculateChecksum computes CRC32 checksum of data
// Returns hex-encoded string representation (8 characters)
func calculateChecksum(data []byte) string {
	checksum := crc32.ChecksumIEEE(data)
	return fmt.Sprintf("%08x", checksum)
}
