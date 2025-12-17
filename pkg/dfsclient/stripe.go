package dfsclient

// Stripe represents a RAID-5 stripe with 2 data chunks and 1 parity chunk
type Stripe struct {
	StripeNum   int
	DataChunk1  []byte
	DataChunk2  []byte
	ParityChunk []byte
	ChunkIDs    [3]string
	Checksums   [3]string
	Servers     [3]string
}

func padChunk(chunk []byte, target int) []byte {
	if len(chunk) >= target {
		return chunk
	}
	p := make([]byte, target)
	copy(p, chunk)
	return p
}

// calculateParity computes XOR-based parity for RAID-5
func calculateParity(chunk1, chunk2 []byte) []byte {
	maxLen := len(chunk1)
	if len(chunk2) > maxLen {
		maxLen = len(chunk2)
	}
	parity := make([]byte, maxLen)
	for i := 0; i < maxLen; i++ {
		var b1, b2 byte
		if i < len(chunk1) {
			b1 = chunk1[i]
		}
		if i < len(chunk2) {
			b2 = chunk2[i]
		}
		parity[i] = b1 ^ b2
	}
	return parity
}
