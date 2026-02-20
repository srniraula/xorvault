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

// TestStreamFileInStripes_SmallFile tests files smaller than 1MB (CHUNK_SIZE)
// This test verifies the fix for the EOF issue when chunk2 has no data
func TestStreamFileInStripes_SmallFile(t *testing.T) {
	// Test case 1: Very small file (100 bytes)
	t.Run("VerySmallFile_100bytes", func(t *testing.T) {
		testData := []byte("This is a small test file with less than 100 bytes of data for testing purposes.")
		r := bytes.NewReader(testData)
		
		// Prepare stripes map for one stripe
		stripes := make(map[int32]*dfspb.StripeMetadata)
		stripes[1] = &dfspb.StripeMetadata{StripeNum: 1, ChunkIds: []string{"c1", "c2", "p1"}, Servers: []string{"s1", "s2", "s3"}}
		
		stripeChan := make(chan Stripe, 2)
		errChan := make(chan error, 1)
		
		g := &GrpcClient{}
		go g.streamFileInStripes(r, stripes, stripeChan, errChan)
		
		// Should produce exactly 1 stripe without errors
		count := 0
		var stripe Stripe
		for s := range stripeChan {
			count++
			stripe = s
		}
		
		// Check for errors
		select {
		case err := <-errChan:
			if err != nil {
				t.Fatalf("unexpected error for small file: %v", err)
			}
		default:
		}
		
		if count != 1 {
			t.Fatalf("expected 1 stripe for small file, got %d", count)
		}
		
		// Verify stripe content
		if stripe.StripeNum != 1 {
			t.Fatalf("expected stripe number 1, got %d", stripe.StripeNum)
		}
		
		// chunk1 should contain the data (padded to CHUNK_SIZE)
		if len(stripe.DataChunk1) != CHUNK_SIZE {
			t.Fatalf("expected chunk1 size %d, got %d", CHUNK_SIZE, len(stripe.DataChunk1))
		}
		
		// chunk2 should be empty (padded to CHUNK_SIZE with zeros)
		if len(stripe.DataChunk2) != CHUNK_SIZE {
			t.Fatalf("expected chunk2 size %d, got %d", CHUNK_SIZE, len(stripe.DataChunk2))
		}
		
		// Original data should be at beginning of chunk1
		if !bytes.Equal(testData, stripe.DataChunk1[:len(testData)]) {
			t.Fatalf("data mismatch in chunk1")
		}
		
		// chunk2 should be all zeros after padding
		expectedEmptyChunk := make([]byte, CHUNK_SIZE)
		if !bytes.Equal(stripe.DataChunk2, expectedEmptyChunk) {
			t.Fatalf("chunk2 should be all zeros for small file")
		}
	})
	
	// Test case 2: Medium small file (500KB)  
	t.Run("MediumSmallFile_500KB", func(t *testing.T) {
		testData := bytes.Repeat([]byte{0xBB}, 500*1024) // 500KB
		r := bytes.NewReader(testData)
		
		stripes := make(map[int32]*dfspb.StripeMetadata)
		stripes[1] = &dfspb.StripeMetadata{StripeNum: 1, ChunkIds: []string{"c1", "c2", "p1"}, Servers: []string{"s1", "s2", "s3"}}
		
		stripeChan := make(chan Stripe, 2)
		errChan := make(chan error, 1)
		
		g := &GrpcClient{}
		go g.streamFileInStripes(r, stripes, stripeChan, errChan)
		
		count := 0
		for range stripeChan {
			count++
		}
		
		// Check for errors
		select {
		case err := <-errChan:
			if err != nil {
				t.Fatalf("unexpected error for 500KB file: %v", err)
			}
		default:
		}
		
		if count != 1 {
			t.Fatalf("expected 1 stripe for 500KB file, got %d", count)
		}
	})
}
