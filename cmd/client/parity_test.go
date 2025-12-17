package main

import (
	"testing"
)

func TestCalculateParity(t *testing.T) {
	chunk1 := []byte{0xFF, 0xAA}
	chunk2 := []byte{0x0F, 0x55}

	parity := calculateParity(chunk1, chunk2)

	// Expected: 0xFF ^ 0x0F = 0xF0, 0xAA ^ 0x55 = 0xFF
	expected := []byte{0xF0, 0xFF}

	if len(parity) != len(expected) {
		t.Fatalf("Expected length %d, got %d", len(expected), len(parity))
	}

	for i := range expected {
		if parity[i] != expected[i] {
			t.Errorf("Byte %d: expected 0x%02X, got 0x%02X", i, expected[i], parity[i])
		}
	}

	t.Logf("✓ Parity calculation correct: %#v", parity)
}
