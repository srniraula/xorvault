package dfsclient

import (
	"testing"
)

func TestChecksumCRC32(t *testing.T) {
	data := []byte("hello world")
	cs := calculateChecksum(data)
	if cs == "" {
		t.Fatalf("expected non-empty checksum")
	}
	// known CRC32 checksum for "hello world" is 0d4a1185
	if cs != "0d4a1185" {
		t.Fatalf("unexpected checksum: %s", cs)
	}
}
