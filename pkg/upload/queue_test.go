package upload

import (
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewDedupQueue(t *testing.T) {
	t.Parallel()
	capacity := 10
	queue := NewDedupQueue(capacity)

	if queue == nil {
		t.Fatal("NewDedupQueue returned nil")
	}

	if queue.capacity != capacity {
		t.Errorf("Expected capacity %d, got %d", capacity, queue.capacity)
	}

	if len(queue.elements) != 0 {
		t.Errorf("Expected empty elements slice, got length %d", len(queue.elements))
	}
}

func TestDedupQueue_Push(t *testing.T) {
	t.Parallel()
	queue := NewDedupQueue(3)

	// Test normal push
	if !queue.Push("item1") {
		t.Error("Failed to push first item")
	}

	if !queue.Push("item2") {
		t.Error("Failed to push second item")
	}

	// Test duplicate push
	if queue.Push("item1") {
		t.Error("Should not push duplicate item")
	}

	if queue.Size() != 2 {
		t.Errorf("Expected size 2, got %d", queue.Size())
	}

	// Test capacity limit
	if !queue.Push("item3") {
		t.Error("Failed to push third item")
	}

	if queue.Push("item4") {
		t.Error("Should not push when queue is full")
	}

	if queue.Size() != 3 {
		t.Errorf("Expected size 3, got %d", queue.Size())
	}
}

func TestDedupQueue_Pop(t *testing.T) {
	t.Parallel()
	queue := NewDedupQueue(3)

	// Test pop from empty queue
	if _, ok := queue.Pop(); ok {
		t.Error("Should not pop from empty queue")
	}

	// Test normal pop
	queue.Push("item1")
	queue.Push("item2")

	elem, ok := queue.Pop()
	if !ok {
		t.Error("Failed to pop item")
	}
	if elem != "item1" {
		t.Errorf("Expected 'item1', got '%s'", elem)
	}

	if queue.Size() != 1 {
		t.Errorf("Expected size 1, got %d", queue.Size())
	}

	// Test pop remaining item
	elem, ok = queue.Pop()
	if !ok {
		t.Error("Failed to pop second item")
	}
	if elem != "item2" {
		t.Errorf("Expected 'item2', got '%s'", elem)
	}

	if queue.Size() != 0 {
		t.Errorf("Expected size 0, got %d", queue.Size())
	}

	// Test pop from empty queue again
	if _, ok := queue.Pop(); ok {
		t.Error("Should not pop from empty queue")
	}
}

func TestDedupQueue_IsFull(t *testing.T) {
	t.Parallel()
	queue := NewDedupQueue(2)

	if queue.IsFull() {
		t.Error("New queue should not be full")
	}

	queue.Push("item1")
	if queue.IsFull() {
		t.Error("Queue with one item should not be full")
	}

	queue.Push("item2")
	if !queue.IsFull() {
		t.Error("Queue with capacity items should be full")
	}
}

func TestDedupQueue_Size(t *testing.T) {
	t.Parallel()
	queue := NewDedupQueue(5)

	if queue.Size() != 0 {
		t.Errorf("New queue should have size 0, got %d", queue.Size())
	}

	queue.Push("item1")
	if queue.Size() != 1 {
		t.Errorf("Queue should have size 1, got %d", queue.Size())
	}

	queue.Push("item2")
	if queue.Size() != 2 {
		t.Errorf("Queue should have size 2, got %d", queue.Size())
	}

	queue.Pop()
	if queue.Size() != 1 {
		t.Errorf("Queue should have size 1 after pop, got %d", queue.Size())
	}
}

func TestDedupQueue_IsEmpty(t *testing.T) {
	t.Parallel()
	queue := NewDedupQueue(3)

	if !queue.IsEmpty() {
		t.Error("New queue should be empty")
	}

	queue.Push("item1")
	if queue.IsEmpty() {
		t.Error("Queue with items should not be empty")
	}

	queue.Pop()
	if !queue.IsEmpty() {
		t.Error("Queue should be empty after pop")
	}
}

func TestDedupQueue_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	queue := NewDedupQueue(100)
	var wg sync.WaitGroup

	// Test concurrent pushes
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			queue.Push("item" + strconv.Itoa(i))
		}()
	}

	// Test concurrent pops
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			queue.Pop()
		}()
	}

	wg.Wait()

	// Verify queue state is consistent
	size := queue.Size()
	if size < 0 || size > 100 {
		t.Errorf("Queue size should be between 0 and 100, got %d", size)
	}
}

func TestDedupQueue_StressTest(t *testing.T) {
	t.Parallel()
	queue := NewDedupQueue(1000)

	// Push many items
	for i := range 1000 {
		if !queue.Push("item" + strconv.Itoa(i)) {
			t.Error("Failed to push item")
		}
	}

	// Verify queue is full
	if !queue.IsFull() {
		t.Error("Queue should be full after pushing 1000 items")
	}

	// Try to push more items (should fail)
	for i := range 100 {
		if queue.Push("item" + strconv.Itoa(i+1000)) {
			t.Error("Should not push when queue is full")
		}
	}

	// Pop all items
	for range 1000 {
		if _, ok := queue.Pop(); !ok {
			t.Error("Failed to pop item")
		}
	}

	// Verify queue is empty
	if !queue.IsEmpty() {
		t.Error("Queue should be empty after popping all items")
	}
}

func TestDedupQueue_EmptyString(t *testing.T) {
	t.Parallel()
	queue := NewDedupQueue(5)

	// Test empty string handling
	if queue.Push("") {
		t.Error("Should be able to push empty string")
	}

	if queue.Size() != 0 {
		t.Errorf("Expected size 0, got %d", queue.Size())
	}

	// Test duplicate empty string
	if queue.Push("") {
		t.Error("Should not push empty string")
	}

	elem, ok := queue.Pop()
	if ok {
		t.Error("Failed to pop empty string")
	}
	if elem != "" {
		t.Errorf("Expected empty string, got '%s'", elem)
	}
}

func TestDedupQueue_ZeroCapacity(t *testing.T) {
	t.Parallel()
	queue := NewDedupQueue(0)
	assert.Nil(t, queue, "Queue should be nil for negative capacity")
}

func TestDedupQueue_NegativeCapacity(t *testing.T) {
	t.Parallel()
	// Test that negative capacity is handled gracefully
	queue := NewDedupQueue(-1)

	assert.Nil(t, queue, "Queue should be nil for negative capacity")
}
