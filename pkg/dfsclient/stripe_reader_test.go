package dfsclient

import (
	"bytes"
	"dfs-project/dfspb"
	"testing"
)

func TestStreamFileInStripes(t *testing.T) {
	// prepare a small reader of size 1.5 * CHUNK_SIZE => 2 stripes (last stripe with one data chunk)
	buf := bytes.Repeat([]byte{0xAA}, CHUNK_SIZE+CHUNK_SIZE/2)
	r := bytes.NewReader(buf)

	// Prepare stripes map for two stripes
	stripes := make(map[int32]*dfspb.StripeMetadata)
	stripes[1] = &dfspb.StripeMetadata{StripeNum: 1, ChunkIds: []string{"c1", "c2", "p1"}, Servers: []string{"s1", "s2", "s3"}}
	stripes[2] = &dfspb.StripeMetadata{StripeNum: 2, ChunkIds: []string{"c3", "", "p2"}, Servers: []string{"s1", "", "s3"}}

	stripeChan := make(chan Stripe, 4)
	errChan := make(chan error, 1)

	g := &GrpcClient{} // not used by stream
	go g.streamFileInStripes(r, stripes, stripeChan, errChan)

	count := 0
	for s := range stripeChan {
		count++
		if s.StripeNum == 1 {
			if s.ChunkIDs[0] != "c1" {
				t.Fatalf("unexpected chunk id")
			}
		}
	}
	// With input size 1.5*CHUNK_SIZE, streamFileInStripes produces 1 stripe (partial second chunk ends the stream)
	if count != 1 {
		t.Fatalf("expected 1 stripe, got %d", count)
	}
}
