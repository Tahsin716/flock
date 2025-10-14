package flock

import (
	"runtime"
	"sync/atomic"
	"unsafe"
)

// cacheLinePad prevents false sharing between hot fields
type cacheLinePad struct {
	_ [64]byte
}

// lockFreeQueue is a bounded, lock-free, MPSC queue
// Optimized for low contention with exponential backoff for high contention
type lockFreeQueue struct {
	_ cacheLinePad

	// head is only modified by single consumer
	head uint64

	_ cacheLinePad

	// tail is modified by multiple producers via CAS
	tail uint64

	_ cacheLinePad

	buffer    []unsafe.Pointer
	mask      uint64
	queueSize uint64
}

// newQueue creates a new bounded lock-free MPSC queue
// Capacity must be a power of two
func newQueue(capacity int) *lockFreeQueue {
	if capacity <= 0 || capacity&(capacity-1) != 0 {
		panic("capacity must be a power of two and > 0")
	}

	return &lockFreeQueue{
		buffer:    make([]unsafe.Pointer, capacity),
		mask:      uint64(capacity - 1),
		queueSize: uint64(capacity),
		head:      0,
		tail:      0,
	}
}

// tryPush attempts to push a task to the queue (MPSC-safe)
// Returns true if successful, false if full
// Uses exponential backoff to reduce contention
func (q *lockFreeQueue) tryPush(task func()) bool {
	if task == nil {
		return false
	}

	taskPtr := unsafe.Pointer(&task)
	const maxAttempts = 128

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Load tail and head
		tail := atomic.LoadUint64(&q.tail)
		head := atomic.LoadUint64(&q.head)

		// Check if full (leave one slot empty to distinguish full vs empty)
		if tail-head >= q.queueSize-1 {
			return false
		}

		// Try to claim this slot
		if atomic.CompareAndSwapUint64(&q.tail, tail, tail+1) {
			// Successfully claimed the slot
			index := tail & q.mask

			// Store the task
			atomic.StorePointer(&q.buffer[index], taskPtr)
			return true
		}

		// CAS failed, apply backoff strategy
		if attempt < 4 {
			// Phase 1: Immediate retry (1-4 attempts)
			// Best for low contention
			runtime.Gosched()

		} else if attempt < 16 {
			// Phase 2: Light backoff (5-16 attempts)
			// Yield CPU multiple times
			for i := 0; i < (1 << (attempt - 4)); i++ {
				runtime.Gosched()
			}

		} else if attempt < 64 {
			// Phase 3: Medium backoff (17-64 attempts)
			// More aggressive yielding
			for i := 0; i < 8; i++ {
				runtime.Gosched()
			}

			// Occasionally refresh head to avoid false "full" reads
			if attempt%8 == 0 {
				head = atomic.LoadUint64(&q.head)
				tail = atomic.LoadUint64(&q.tail)
				if tail-head >= q.queueSize-1 {
					return false // Really full
				}
			}

		} else {
			// Phase 4: Heavy backoff (65+ attempts)
			// Last resort before giving up
			for i := 0; i < 16; i++ {
				runtime.Gosched()
			}

			// Final check if actually full
			head = atomic.LoadUint64(&q.head)
			tail = atomic.LoadUint64(&q.tail)
			if tail-head >= q.queueSize-1 {
				return false
			}
		}
	}

	// After max attempts, do final check
	tail := atomic.LoadUint64(&q.tail)
	head := atomic.LoadUint64(&q.head)
	return tail-head < q.queueSize-1
}

// pop removes and returns one task (single consumer only - not thread-safe)
func (q *lockFreeQueue) pop() func() {
	// Load head first
	head := atomic.LoadUint64(&q.head)

	// Then load tail
	tail := atomic.LoadUint64(&q.tail)

	// Check if empty
	if head >= tail {
		return nil
	}

	// Get the task
	index := head & q.mask

	// Load the task
	taskPtr := atomic.LoadPointer(&q.buffer[index])

	if taskPtr == nil {
		// This shouldn't happen in correct usage
		return nil
	}

	// Clear the slot
	atomic.StorePointer(&q.buffer[index], nil)

	// Advance head
	atomic.StoreUint64(&q.head, head+1)

	// Convert back to function
	task := *(*func())(taskPtr)
	return task
}

// size returns the approximate queue length
// This is a snapshot and may be stale during concurrent operations
func (q *lockFreeQueue) size() int {
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)

	if tail < head {
		return 0
	}

	return int(tail - head)
}

// isEmpty checks if the queue appears empty
func (q *lockFreeQueue) isEmpty() bool {
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)
	return head >= tail
}

// isFull checks if the queue appears full
func (q *lockFreeQueue) isFuoll() bool {
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)
	return tail-head >= q.queueSize-1
}

// capacity returns the queue's maximum capacity
func (q *lockFreeQueue) capacity() int {
	return int(q.queueSize)
}

// reset clears the queue (not thread-safe - use only when no concurrent operations)
func (q *lockFreeQueue) reset() {
	for i := range q.buffer {
		atomic.StorePointer(&q.buffer[i], nil)
	}
	atomic.StoreUint64(&q.head, 0)
	atomic.StoreUint64(&q.tail, 0)
}
