package flock

import (
	"runtime"
	"sync/atomic"
)

// cacheLinePad prevents false sharing between hot fields
type cacheLinePad struct {
	_ [64]byte
}

// lockFreeQueue is a bounded, lock-free, MPSC queue
// Optimized for performance while maintaining correctness
type lockFreeQueue struct {
	_ cacheLinePad

	// head is only modified by consumer
	head uint64

	_ cacheLinePad

	// tail is modified by multiple producers via CAS
	tail uint64

	_ cacheLinePad

	buffer    []atomic.Pointer[func()]
	sequences []uint64
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
		buffer:    make([]atomic.Pointer[func()], capacity),
		sequences: make([]uint64, capacity),
		mask:      uint64(capacity - 1),
		queueSize: uint64(capacity),
	}

	for i := range q.sequences {
		q.sequences[i] = uint64(i) // Initialize sequence numbers
		q.buffer[i].Store(nil)     // Initialize buffer slots
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

	const maxAttempts = 128

	for attempt := 0; attempt < maxAttempts; attempt++ {
		tail := atomic.LoadUint64(&q.tail)
		index := tail & q.mask

		// Atomic load (synchronizes with consumer's sequence update)
		// This ensures we see the latest sequence number
		// and prevents ABA issues
		seq := atomic.LoadUint64(&q.sequences[index])

		diff := int64(seq) - int64(tail) // handle wraparound zero

		if diff == 0 {
			if atomic.CompareAndSwapUint64(&q.tail, tail, tail+1) {
				// Store pointer atomically (ensures visibility)
				q.buffer[index].Store(&task)

				// Release barrier: ensures buffer write completes before sequence update
				atomic.StoreUint64(&q.sequences[index], tail+1)
				return true
			}
		} else if diff < 0 {
			return false
		}

		runtime.Gosched()
	}

	return false
}

// pop removes one task (single consumer)
func (q *lockFreeQueue) pop() func() {
	// Atomic load to see latest head
	head := atomic.LoadUint64(&q.head)
	index := head & q.mask

	// Atomic load (synchronizes with producer's sequence update)
	seq := atomic.LoadUint64(&q.sequences[index])

	if seq != head+1 {
		return nil
	}

	// Atomic load (ensures we see producer's buffer write)
	taskPtr := q.buffer[index].Load()

	// remove task from buffer
	q.buffer[index].Store(nil)

	// Update sequence atomically (producers read this)
	atomic.StoreUint64(&q.sequences[index], head+q.queueSize)

	// Advance head
	atomic.StoreUint64(&q.head, head+1)

	task := *taskPtr
	return task
}

// size returns approximate queue length
func (q *lockFreeQueue) size() int {
	// Load with atomic to see latest values
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

// isEmpty checks if queue appears empty
func (q *lockFreeQueue) isEmpty() bool {
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)
	return head >= tail
}

// isFull checks if queue appears full
func (q *lockFreeQueue) isFull() bool {
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)
	return tail-head >= q.queueSize-1
}

// capacity returns queue capacity
func (q *lockFreeQueue) capacity() int {
	return int(q.queueSize)
}

// reset clears the queue (not thread-safe)
func (q *lockFreeQueue) reset() {
	for i := range q.buffer {
		q.buffer[i].Store(nil)
		q.sequences[i] = uint64(i)
	}
	q.head = 0
	q.tail = 0
}
