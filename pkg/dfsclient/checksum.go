package dfsclient

import (
	"fmt"
	"hash/crc32"
)

func calculateChecksum(data []byte) string {
	checksum := crc32.ChecksumIEEE(data)
	return fmt.Sprintf("%08x", checksum)
}
