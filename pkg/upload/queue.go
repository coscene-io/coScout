package upload

import (
	"sync"
)

type DedupQueue struct {
	elements []string
	capacity int
	mu       sync.RWMutex
}

func NewDedupQueue(capacity int) *DedupQueue {
	if capacity <= 0 {
		return nil
	}

	return &DedupQueue{
		elements: make([]string, 0),
		capacity: capacity,
	}
}

// Push adds a new element to the queue, ignores if element already exists or queue is full.
func (dq *DedupQueue) Push(record string) bool {
	dq.mu.Lock()
	defer dq.mu.Unlock()

	// Check if queue is full.
	if len(dq.elements) >= dq.capacity {
		return false
	}

	if record == "" {
		return false
	}

	// Check if element already exists.
	for _, elem := range dq.elements {
		if elem == record {
			return false
		}
	}

	// Add new element
	dq.elements = append(dq.elements, record)
	return true
}

// Pop removes and returns the first element from the queue.
func (dq *DedupQueue) Pop() (string, bool) {
	dq.mu.Lock()
	defer dq.mu.Unlock()

	if len(dq.elements) == 0 {
		return "", false
	}

	elem := dq.elements[0]
	dq.elements = dq.elements[1:]
	return elem, true
}

// IsFull checks if the queue is at maximum capacity.
func (dq *DedupQueue) IsFull() bool {
	dq.mu.RLock()
	defer dq.mu.RUnlock()

	return len(dq.elements) >= dq.capacity
}

// Size returns the current number of elements in the queue.
func (dq *DedupQueue) Size() int {
	dq.mu.RLock()
	defer dq.mu.RUnlock()

	return len(dq.elements)
}

// IsEmpty checks if the queue has no elements.
func (dq *DedupQueue) IsEmpty() bool {
	dq.mu.RLock()
	defer dq.mu.RUnlock()

	return dq.Size() == 0
}

// Clear removes all elements from the queue.
func (dq *DedupQueue) Clear() {
	dq.mu.Lock()
	defer dq.mu.Unlock()
}
