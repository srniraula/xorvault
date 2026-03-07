package dfsclient

import (
	"context"
	"testing"
)

func TestUploadStripesStreamingSuccess(t *testing.T) {
	// Prepare a stripe channel with one stripe
	stripe := Stripe{StripeNum: 1, ChunkIDs: [3]string{"c1", "c2", "p1"}, Servers: [3]string{"s1", "s2", "s3"}, DataChunk1: []byte{1}, DataChunk2: []byte{2}, ParityChunk: []byte{3}}
	ch := make(chan Stripe, 1)
	ch <- stripe
	close(ch)

	ack := NewAckQueue()

	// stub uploader
	orig := chunkUploader
	chunkUploader = func(ctx context.Context, serverAddr string, chunkID string, data []byte, clientID int64, username string) error {
		return nil
	}
	defer func() { chunkUploader = orig }()

	g := &GrpcClient{}
	ok, err := g.uploadStripesStreaming(ch, ack, 42, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ok) != 3 {
		t.Fatalf("expected 3 successful chunks, got %d", len(ok))
	}
	if !ack.IsEmpty() {
		t.Fatalf("expected ack queue empty after successful uploads")
	}
}

func TestUploadStripesStreamingFailure(t *testing.T) {
	stripe := Stripe{StripeNum: 1, ChunkIDs: [3]string{"c1", "c2", "p1"}, Servers: [3]string{"s1", "s2", "s3"}, DataChunk1: []byte{1}, DataChunk2: []byte{2}, ParityChunk: []byte{3}}
	ch := make(chan Stripe, 1)
	ch <- stripe
	close(ch)

	ack := NewAckQueue()

	orig := chunkUploader
	chunkUploader = func(ctx context.Context, serverAddr string, chunkID string, data []byte, clientID int64, username string) error {
		if chunkID == "c2" {
			return context.Canceled
		}
		return nil
	}
	defer func() { chunkUploader = orig }()

	g := &GrpcClient{}
	_, err := g.uploadStripesStreaming(ch, ack, 42, "")
	if err == nil {
		t.Fatalf("expected error due to one failed chunk")
	}
}
