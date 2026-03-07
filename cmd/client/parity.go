package main

// calculateParity computes XOR-based parity for RAID-4
// Takes two data chunks and returns parity chunk
func calculateParity(chunk1, chunk2 []byte) []byte {
	// Use the longer chunk size as basis
	maxLen := len(chunk1)
	if len(chunk2) > maxLen {
		maxLen = len(chunk2)
	}

	parity := make([]byte, maxLen)

	// XOR byte by byte
	for i := 0; i < maxLen; i++ {
		var byte1, byte2 byte
		if i < len(chunk1) {
			byte1 = chunk1[i]
		}
		if i < len(chunk2) {
			byte2 = chunk2[i]
		}
		parity[i] = byte1 ^ byte2
	}

	return parity
}

// padChunk pads a chunk to CHUNK_SIZE with zeros
// Used when last stripe has only 1 chunk
func padChunk(chunk []byte, targetSize int) []byte {
	if len(chunk) >= targetSize {
		return chunk
	}

	padded := make([]byte, targetSize)
	copy(padded, chunk)
	// Remaining bytes are already zero from make()
	return padded
}
