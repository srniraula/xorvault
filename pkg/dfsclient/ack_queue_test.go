package dfsclient

import (
	"testing"
)

func TestAckQueueBasic(t *testing.T) {
	q := NewAckQueue()
	if !q.IsEmpty() {
		t.Fatalf("expected empty ack queue")
	}
	q.Add("a")
	q.Add("b")
	if q.IsEmpty() {
		t.Fatalf("expected non-empty ack queue")
	}
	pending := q.GetPending()
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}
	q.Remove("a")
	pending = q.GetPending()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending after remove, got %d", len(pending))
	}
}
