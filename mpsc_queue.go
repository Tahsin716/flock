package flock

import (
	"sync/atomic"
	"unsafe"
)

// CacheLinePad prevents false sharing by padding to cache line size (64 bytes)
type CacheLinePad struct {
	_ [64]byte
}

// LockFreeQueue is a bounded, lock-free, MPSC (Multi-Producer Single-Consumer) queue
// Multiple goroutines can safely push concurrently, but only one should pop
type LockFreeQueue struct {
	// Padding to prevent false sharing
	_ CacheLinePad

	// head is the consumer index (only modified by single consumer/worker)
	// Always incremented, never decremented
	head uint64

	// Padding between head and tail (different cache lines)
	_ CacheLinePad

	// tail is the producer index (modified by multiple submitters via CAS)
	// Always incremented, never decremented
	tail uint64

	// Padding after tail
	_ CacheLinePad

	// buffer holds the actual task functions
	buffer []unsafe.Pointer

	// mask is size-1, used for fast modulo via bitwise AND
	// Only works because size is power of 2
	mask uint64

	// size is the capacity of the queue
	size uint64
}

// New creates a new lock-free queue with the given capacity
// Capacity MUST be a power of 2 for the bitwise modulo optimization to work
func New(capacity int) *LockFreeQueue {
	if capacity <= 0 {
		panic("capacity must be positive")
	}

	// Ensure capacity is power of 2
	if capacity&(capacity-1) != 0 {
		panic("capacity must be a power of 2")
	}

	return &LockFreeQueue{
		buffer: make([]unsafe.Pointer, capacity),
		mask:   uint64(capacity - 1),
		size:   uint64(capacity),
		head:   0,
		tail:   0,
	}
}

// TryPush attempts to push a task to the queue
// Returns true if successful, false if queue is full
// Thread-safe for multiple producers via CAS
func (q *LockFreeQueue) TryPush(task func()) bool {
	if task == nil {
		return false
	}

	taskPtr := unsafe.Pointer(&task)

	for {
		// Load tail first, then head
		tail := atomic.LoadUint64(&q.tail)
		head := atomic.LoadUint64(&q.head)

		// Check if full (leave one slot empty)
		if tail-head >= q.size-1 {
			return false
		}

		// Try to claim this slot
		if atomic.CompareAndSwapUint64(&q.tail, tail, tail+1) {
			// We own this slot now
			index := tail & q.mask

			// Store the task with release semantics
			// This ensures the task is visible before the tail increment is seen
			atomic.StorePointer(&q.buffer[index], taskPtr)

			return true
		}
		// CAS failed, retry
	}
}

// Pop removes and returns a task from the queue
// Returns nil if queue is empty
// NOT thread-safe - only single consumer should call this
func (q *LockFreeQueue) Pop() func() {
	// Load head first
	head := atomic.LoadUint64(&q.head)

	// Then load tail with acquire semantics
	tail := atomic.LoadUint64(&q.tail)

	// Check if empty
	if head >= tail {
		return nil
	}

	// Get the index
	index := head & q.mask

	// Load the task with acquire semantics
	// This ensures we see the task that was stored by the producer
	taskPtr := atomic.LoadPointer(&q.buffer[index])

	if taskPtr == nil {
		// This shouldn't happen in correct usage
		// but be defensive
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

// Size returns the current number of items in the queue
// This is an estimate and may be stale due to concurrent operations
func (q *LockFreeQueue) Size() int {
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)

	if tail < head {
		return 0
	}

	return int(tail - head)
}

// IsEmpty returns true if the queue appears empty
func (q *LockFreeQueue) IsEmpty() bool {
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)
	return head >= tail
}

// Capacity returns the maximum capacity of the queue
func (q *LockFreeQueue) Capacity() int {
	return int(q.size)
}

// IsFull returns true if the queue appears full
func (q *LockFreeQueue) IsFull() bool {
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)
	return tail-head >= q.size-1
}

// Reset clears all items from the queue
// NOT thread-safe - only call when no concurrent operations
func (q *LockFreeQueue) Reset() {
	// Clear all slots
	for i := range q.buffer {
		atomic.StorePointer(&q.buffer[i], nil)
	}

	// Reset indices
	atomic.StoreUint64(&q.head, 0)
	atomic.StoreUint64(&q.tail, 0)
}
