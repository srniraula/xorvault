package main

import (
	"sync"
)

// AckQueue tracks pending chunk uploads
// Thread-safe queue for managing upload acknowledgments
type AckQueue struct {
	mu      sync.Mutex
	pending map[string]bool // chunk_id -> waiting for ack
}

// NewAckQueue creates a new acknowledgment queue
func NewAckQueue() *AckQueue {
	return &AckQueue{
		pending: make(map[string]bool),
	}
}

// Add adds a chunk to the pending queue
func (q *AckQueue) Add(chunkID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pending[chunkID] = true
}

// Remove removes a chunk from the pending queue (ack received)
func (q *AckQueue) Remove(chunkID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.pending, chunkID)
}

// IsEmpty checks if all chunks have been acknowledged
func (q *AckQueue) IsEmpty() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending) == 0
}

// Size returns the number of pending chunks
func (q *AckQueue) Size() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}

// GetPending returns list of pending chunk IDs
func (q *AckQueue) GetPending() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	pending := make([]string, 0, len(q.pending))
	for chunkID := range q.pending {
		pending = append(pending, chunkID)
	}
	return pending
}
