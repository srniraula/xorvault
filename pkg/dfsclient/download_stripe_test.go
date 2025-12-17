package dfsclient

import (
	"testing"
)

func TestDownloadStripeAndReconstruct(t *testing.T) {
	// Prepare a stripe with missing data chunk2; provide data1 and parity
	info := DownloadStripeInfo{
		StripeNum: 1,
		DataChunk1: ChunkServerPair{ChunkID: "d1", Server: "s1"},
		DataChunk2: ChunkServerPair{ChunkID: "d2", Server: "s2"},
		ParityChunk: ChunkServerPair{ChunkID: "p1", Server: "s3"},
	}

	// stub downloader
	orig := chunkDownloader
	chunkDownloader = func(g *GrpcClient, chunkID, serverAddr string, clientID int64, isData1, isData2, isParity bool) DownloadedChunk {
		if isData1 { return DownloadedChunk{ChunkID: chunkID, Data: []byte{1,2,3}, Success: true, IsData1: true} }
		if isParity { return DownloadedChunk{ChunkID: chunkID, Data: []byte{1^0,2^0,3^0}, Success: true, IsParity: true} }
		return DownloadedChunk{ChunkID: chunkID, Success: false, Error: nil}
	}
	defer func(){ chunkDownloader = orig }()

	g := &GrpcClient{}
	sd := g.downloadStripe(info, 42)
	if sd.ChunksOK != 2 {
		t.Fatalf("expected 2 chunks ok, got %d", sd.ChunksOK)
	}
	// reconstruct
	err := reconstructMissingChunk(&sd)
	if err != nil {
		t.Fatalf("reconstruction failed: %v", err)
	}
	if sd.DataChunk2 == nil {
		t.Fatalf("expected DataChunk2 reconstructed")
	}
}
