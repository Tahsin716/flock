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
	sequences []uint64 // Per-slot sequence numbers
	mask      uint64
	queueSize uint64
}

// newQueue creates a new bounded lock-free MPSC queue
// Capacity must be a power of two
func newQueue(capacity int) *lockFreeQueue {
	if capacity <= 0 || capacity&(capacity-1) != 0 {
		panic("capacity must be a power of two and > 0")
	}

	q := &lockFreeQueue{
		buffer:    make([]unsafe.Pointer, capacity),
		sequences: make([]uint64, capacity),
		mask:      uint64(capacity - 1),
		queueSize: uint64(capacity),
		head:      0,
		tail:      0,
	}

	// Initialize sequences
	for i := range q.sequences {
		atomic.StoreUint64(&q.sequences[i], uint64(i))
	}

	return q
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
		// Load current tail
		tail := atomic.LoadUint64(&q.tail)
		index := tail & q.mask

		// Check the sequence number for this slot
		seq := atomic.LoadUint64(&q.sequences[index])

		// Calculate the difference (handles wrap-around)
		diff := int64(seq) - int64(tail)

		if diff == 0 {
			// Slot is ready for writing
			// Try to claim this slot by advancing tail
			if atomic.CompareAndSwapUint64(&q.tail, tail, tail+1) {
				// Successfully claimed - now write the task
				atomic.StorePointer(&q.buffer[index], taskPtr)

				// Update sequence to signal data is ready
				// Consumer expects sequence to be tail + 1
				atomic.StoreUint64(&q.sequences[index], tail+1)
				return true
			}
		} else if diff < 0 {
			return false // queue is full
		}

		// Simple backoff
		runtime.Gosched()
	}

	return false
}

// pop removes and returns one task (single consumer only - not thread-safe)
func (q *lockFreeQueue) pop() func() {
	head := atomic.LoadUint64(&q.head)
	index := head & q.mask

	// Check the sequence number
	seq := atomic.LoadUint64(&q.sequences[index])

	// We expect sequence to be head + 1 (data ready)
	if seq != head+1 {
		// Data not ready yet, or slot is empty
		return nil
	}

	// Load the task
	taskPtr := atomic.LoadPointer(&q.buffer[index])

	if taskPtr == nil {
		// This shouldn't happen in correct usage
		panic("[Flock] Error in MPSC queue design, taskPtr is nil")
	}

	// Clear the slot
	atomic.StorePointer(&q.buffer[index], nil)

	// Update sequence to signal slot is available for writing
	// Next producer expects sequence to be head + queueSize
	atomic.StoreUint64(&q.sequences[index], head+q.queueSize)

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

	size := int(tail - head)
	if size > int(q.queueSize) {
		return int(q.queueSize)
	}
	return size
}

// isEmpty checks if the queue appears empty
func (q *lockFreeQueue) isEmpty() bool {
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)
	return head >= tail
}

// isFull checks if the queue appears full
func (q *lockFreeQueue) isFull() bool {
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
		atomic.StoreUint64(&q.sequences[i], uint64(i))
	}
	atomic.StoreUint64(&q.head, 0)
	atomic.StoreUint64(&q.tail, 0)
}
