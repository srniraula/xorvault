package dfsclient

import "sync"

// AckQueue tracks pending chunk uploads
type AckQueue struct {
	mu      sync.Mutex
	pending map[string]bool
}

// NewAckQueue creates a new acknowledgment queue
func NewAckQueue() *AckQueue {
	return &AckQueue{pending: make(map[string]bool)}
}

func (q *AckQueue) Add(chunkID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pending[chunkID] = true
}

func (q *AckQueue) Remove(chunkID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.pending, chunkID)
}

func (q *AckQueue) IsEmpty() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending) == 0
}

func (q *AckQueue) GetPending() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]string, 0, len(q.pending))
	for k := range q.pending {
		out = append(out, k)
	}
	return out
}
