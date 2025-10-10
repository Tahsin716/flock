package flock

import (
	"sync/atomic"
	"unsafe"
)

// cacheLinePad prevents false sharing by padding to cache line size (64 bytes)
type cacheLinePad struct {
	_ [64]byte
}

// lockFreeQueue is a bounded, lock-free, MPSC (Multi-Producer Single-Consumer) queue
// Multiple goroutines can safely push concurrently, but only one should pop
type lockFreeQueue struct {
	// Padding to prevent false sharing
	_ cacheLinePad

	// head is the consumer index (only modified by single consumer/worker)
	// Always incremented, never decremented
	head uint64

	// Padding between head and tail (different cache lines)
	_ cacheLinePad

	// tail is the producer index (modified by multiple submitters via CAS)
	// Always incremented, never decremented
	tail uint64

	// Padding after tail
	_ cacheLinePad

	// buffer holds the actual task functions
	buffer []unsafe.Pointer

	// mask is size-1, used for fast modulo via bitwise AND
	// Only works because size is power of 2
	mask uint64

	// queueSize is the capacity of the queue
	queueSize uint64
}

// newQueue creates a new lock-free queue with the given capacity
// Capacity MUST be a power of 2 for the bitwise modulo optimization to work
func newQueue(capacity int) *lockFreeQueue {
	if capacity <= 0 {
		panic("capacity must be positive")
	}

	// Ensure capacity is power of 2
	if capacity&(capacity-1) != 0 {
		panic("capacity must be a power of 2")
	}

	return &lockFreeQueue{
		buffer:    make([]unsafe.Pointer, capacity),
		mask:      uint64(capacity - 1),
		queueSize: uint64(capacity),
		head:      0,
		tail:      0,
	}
}

// tryPush attempts to push a task to the queue
// Returns true if successful, false if queue is full
// Thread-safe for multiple producers via CAS
func (q *lockFreeQueue) tryPush(task func()) bool {
	if task == nil {
		return false
	}

	taskPtr := unsafe.Pointer(&task)

	for {
		// Load tail first, then head
		tail := atomic.LoadUint64(&q.tail)
		head := atomic.LoadUint64(&q.head)

		// Check if full (leave one slot empty)
		if tail-head >= q.queueSize-1 {
			return false
		}

		// Try to claim this slot
		if atomic.CompareAndSwapUint64(&q.tail, tail, tail+1) {
			// We own this slot now
			index := tail & q.mask

			// Store the task
			atomic.StorePointer(&q.buffer[index], taskPtr)

			return true
		}
		// CAS failed, retry
	}
}

// pop removes and returns a task from the queue
// Returns nil if queue is empty
// NOT thread-safe - only single consumer should call this
func (q *lockFreeQueue) pop() func() {
	// Load head first
	head := atomic.LoadUint64(&q.head)

	// Then load tail
	tail := atomic.LoadUint64(&q.tail)

	// Check if empty
	if head >= tail {
		return nil
	}

	// Get the index
	index := head & q.mask

	// Load the task
	taskPtr := atomic.LoadPointer(&q.buffer[index])

	if taskPtr == nil {
		// This shouldn't happen in correct usage
		return nil
	}

	// Clear the slot
	atomic.StorePointer(&q.buffer[index], nil)

	// Increment head
	atomic.StoreUint64(&q.head, head+1)

	// Convert back to function
	task := *(*func())(taskPtr)
	return task
}

// size returns the current number of items in the queue
// This is an estimate and may be stale due to concurrent operations
func (q *lockFreeQueue) size() int {
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)

	if tail < head {
		return 0
	}

	return int(tail - head)
}

// isEmpty returns true if the queue appears empty
func (q *lockFreeQueue) isEmpty() bool {
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)
	return head >= tail
}

// capacity returns the maximum capacity of the queue
func (q *lockFreeQueue) capacity() int {
	return int(q.queueSize)
}

// isFull returns true if the queue appears full
func (q *lockFreeQueue) isFull() bool {
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)
	return tail-head >= q.queueSize-1
}

// reset clears all items from the queue
// NOT thread-safe - only call when no concurrent operations
func (q *lockFreeQueue) reset() {
	// Clear all slots
	for i := range q.buffer {
		atomic.StorePointer(&q.buffer[i], nil)
	}

	// Reset indices
	atomic.StoreUint64(&q.head, 0)
	atomic.StoreUint64(&q.tail, 0)
}
