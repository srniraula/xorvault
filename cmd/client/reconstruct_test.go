package main

import (
	"testing"
)

func TestReconstruct_OddChunkScenario(t *testing.T) {
	// Mock 1MB chunks
	chunkSize := 1024 * 1024
	data1 := make([]byte, chunkSize)
	for i := range data1 {
		data1[i] = byte(i % 255)
	}

	// Case 1: Odd chunk file, Parity Dead.
	// We have Data1. We do NOT have Data2 (it doesn't exist). We do NOT have Parity (dead).
	// Logic sees IsData2Expected=false, realizes we have Data1, so we are good!

	stripe := StripeDownload{
		StripeNum:       10,
		DataChunk1:      data1,
		DataChunk2:      nil,
		ParityChunk:     nil,
		ChunksOK:        1,
		IsData2Expected: false,
	}

	err := reconstructMissingChunk(&stripe)

	// This should SUCCESS now
	if err != nil {
		t.Errorf("Expected success, but got error: %v", err)
	}
}
