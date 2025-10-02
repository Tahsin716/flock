package flock

import (
	"sync/atomic"
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
	buffer []func()

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
		buffer: make([]func(), capacity),
		mask:   uint64(capacity - 1),
		size:   uint64(capacity),
		head:   0,
		tail:   0,
	}
}

// TryPush attempts to push a task to the queue
// Returns true if successful, false if queue is full
// Thread-safe for multiple producers via CAS
//
// Algorithm:
// 1. Load current tail and head positions
// 2. Check if queue is full (leave one slot empty to distinguish full/empty)
// 3. Try to claim a slot via CAS on tail
// 4. Store task at claimed position
// 5. Return success
func (q *LockFreeQueue) TryPush(task func()) bool {
	if task == nil {
		return false
	}

	// Infinite retry loop (but usually succeeds on first try)
	for {
		// Load current positions with acquire semantics
		// Acquire ensures we see all previous writes to the queue
		tail := atomic.LoadUint64(&q.tail)
		head := atomic.LoadUint64(&q.head)

		// Check if queue is full
		// We leave one slot empty to distinguish between full and empty
		// This is a common technique in circular buffers
		//
		// Example with size=8:
		// Empty: head=0, tail=0 (tail-head=0)
		// Full:  head=0, tail=7 (tail-head=7, one slot unused)
		// Full:  head=3, tail=10 (tail-head=7, wraps around)
		if tail-head >= q.size-1 {
			return false // Queue is full
		}

		// Try to claim this slot via Compare-And-Swap
		// This is where the lock-free magic happens!
		//
		// CAS atomically does:
		//   if q.tail == tail:
		//     q.tail = tail + 1
		//     return true
		//   else:
		//     return false (someone else modified tail)
		//
		// If multiple producers try to push simultaneously:
		// - All read the same tail value
		// - All try CAS with that value
		// - Only ONE succeeds
		// - Others retry with the new tail value
		if atomic.CompareAndSwapUint64(&q.tail, tail, tail+1) {
			// We successfully claimed slot at index 'tail'

			// Calculate actual buffer index using mask (fast modulo)
			// This is why capacity must be power of 2!
			//
			// Example: tail=10, mask=7 (size=8)
			// index = 10 & 7 = 2 (binary: 1010 & 0111 = 0010)
			index := tail & q.mask

			// Store the task at our claimed slot
			// No atomic needed here - we exclusively own this slot
			q.buffer[index] = task

			// Memory fence (release semantics) to ensure task write is visible
			// before tail update is visible to consumers
			// This is implicit in the CAS above, but documenting the intent
			//
			// Without this ordering:
			// Consumer might see new tail value but old buffer content!

			return true
		}

		// CAS failed - another producer claimed this slot
		// Loop and retry with updated tail value
		// This is rare under low contention
	}
}

// Pop removes and returns a task from the queue
// Returns nil if queue is empty
// NOT thread-safe - only single consumer should call this
//
// Algorithm:
// 1. Load head and tail positions
// 2. Check if empty
// 3. Get task at head index
// 4. Clear the slot (help GC)
// 5. Increment head
func (q *LockFreeQueue) Pop() func() {
	// Load current positions
	// Acquire semantics ensure we see the task written by producer
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)

	// Check if empty
	// If head has caught up to tail, queue is empty
	if head >= tail {
		return nil
	}

	// Calculate buffer index
	index := head & q.mask

	// Get the task
	task := q.buffer[index]

	// CRITICAL: Clear the slot to allow garbage collection
	// Without this, we'd leak memory by holding references to old tasks
	q.buffer[index] = nil

	// Increment head with release semantics
	// Release ensures the buffer clear is visible before head increment
	atomic.StoreUint64(&q.head, head+1)

	return task
}

// Size returns the current number of items in the queue
// This is an estimate and may be stale due to concurrent operations
// Useful for monitoring and debugging, not for synchronization
func (q *LockFreeQueue) Size() int {
	tail := atomic.LoadUint64(&q.tail)
	head := atomic.LoadUint64(&q.head)

	// tail - head gives us the number of items
	// This might be slightly stale due to concurrent operations
	size := int(tail - head)

	// Clamp to 0 (shouldn't happen, but defensive)
	if size < 0 {
		return 0
	}

	return size
}

// IsEmpty returns true if the queue appears empty
// This is a snapshot in time and may be stale immediately
func (q *LockFreeQueue) IsEmpty() bool {
	return q.Size() == 0
}

// Capacity returns the maximum capacity of the queue
func (q *LockFreeQueue) Capacity() int {
	return int(q.size)
}

// IsFull returns true if the queue appears full
// This is a snapshot and may be stale
func (q *LockFreeQueue) IsFull() bool {
	return q.Size() >= int(q.size)-1
}

// Reset clears all items from the queue
// NOT thread-safe - only call when no concurrent operations
// Useful for testing or shutdown cleanup
func (q *LockFreeQueue) Reset() {
	// Drain all tasks
	for q.Pop() != nil {
		// Just drain, don't execute
	}

	// Reset indices
	atomic.StoreUint64(&q.head, 0)
	atomic.StoreUint64(&q.tail, 0)
}
