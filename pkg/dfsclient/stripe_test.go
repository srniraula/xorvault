package dfsclient

import (
	"bytes"
	"testing"
)

func TestPadChunk(t *testing.T) {
	in := []byte{1,2,3}
	p := padChunk(in, 5)
	if len(p) != 5 { t.Fatalf("expected len 5, got %d", len(p)) }
}

func TestCalculateParity(t *testing.T) {
	c1 := []byte{1,2,3}
	c2 := []byte{4,5,6}
	p := calculateParity(c1,c2)
	expected := []byte{1^4,2^5,3^6}
	if !bytes.Equal(p, expected) { t.Fatalf("parity mismatch") }
}
