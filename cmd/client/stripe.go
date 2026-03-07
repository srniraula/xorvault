package main

// Stripe represents a RAID-4 stripe with 2 data chunks and 1 parity chunk
type Stripe struct {
	StripeNum   int
	DataChunk1  []byte
	DataChunk2  []byte
	ParityChunk []byte
	ChunkIDs    [3]string // [chunk1_id, chunk2_id, parity_id]
	Checksums   [3]string // [chunk1_checksum, chunk2_checksum, parity_checksum]
	Servers     [3]string // [server1, server2, server3]
}
