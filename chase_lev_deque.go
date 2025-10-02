package flock

import (
	"sync/atomic"
)

// ChaseLevDeque is a lock-free work-stealing deque
//
// Properties:
// - Owner can push/pop at bottom (LIFO - newest tasks first)
// - Thieves steal from top (FIFO - oldest tasks first)
// - Lock-free coordination between owner and thieves
// - Dynamically resizable
//
// Memory ordering is CRITICAL for correctness!
type ChaseLevDeque struct {
	// Padding to prevent false sharing
	_ CacheLinePad

	// top is the steal end (incremented by thieves via CAS)
	// Multiple thieves compete to increment this
	top int64

	// Padding between top and bottom
	_ CacheLinePad

	// bottom is the owner end (modified only by owner)
	// Owner has exclusive access to bottom
	bottom int64

	// Padding after bottom
	_ CacheLinePad

	// array is the underlying circular buffer
	// Can be replaced with a larger one when full
	// atomic.Value allows lock-free replacement
	array atomic.Value // stores *circularArray
}

// circularArray is the underlying storage for the deque
// Immutable after creation (we create new ones when resizing)
type circularArray struct {
	capacity int64
	buffer   []func()
}

// NewChaseLevDeque creates a new work-stealing deque
func NewChaseLevDeque(initialCapacity int64) *ChaseLevDeque {
	if initialCapacity <= 0 {
		initialCapacity = 16 // Default minimum
	}

	d := &ChaseLevDeque{
		top:    0,
		bottom: 0,
	}

	// Initialize with the initial array
	d.array.Store(newCircularArray(initialCapacity))

	return d
}

// newCircularArray creates a new circular array
func newCircularArray(capacity int64) *circularArray {
	return &circularArray{
		capacity: capacity,
		buffer:   make([]func(), capacity),
	}
}

// get returns the element at the given index
// Uses modulo to wrap around (circular buffer)
func (a *circularArray) get(index int64) func() {
	return a.buffer[index%a.capacity]
}

// put stores an element at the given index
func (a *circularArray) put(index int64, task func()) {
	a.buffer[index%a.capacity] = task
}

// Push adds a task to the bottom (owner only, LIFO end)
// NOT thread-safe - only the owning worker should call this
//
// Algorithm:
// 1. Load bottom and top
// 2. Check if resize needed
// 3. Store task at bottom
// 4. Memory fence (ensure task write visible before bottom increment)
// 5. Increment bottom
//
// Memory Ordering:
// - RELEASE fence ensures task write happens-before bottom increment
// - This synchronizes with thieves' ACQUIRE load of bottom
func (d *ChaseLevDeque) Push(task func()) {
	if task == nil {
		return
	}

	// Load current bottom (relaxed - only we modify it)
	bottom := atomic.LoadInt64(&d.bottom)

	// Load top with acquire (coordinate with thieves)
	top := atomic.LoadInt64(&d.top)

	// Load current array (relaxed)
	array := d.array.Load().(*circularArray)

	// Calculate current size
	size := bottom - top

	// Check if we need to resize
	// Leave one slot empty to avoid ambiguity
	if size >= array.capacity-1 {
		// Need more space - allocate new array
		array = d.resize(bottom, top, array)
		d.array.Store(array) // Atomic replacement
	}

	// Store task at bottom position
	array.put(bottom, task)

	// CRITICAL: Memory fence with RELEASE semantics
	// Ensures task write is visible before bottom increment
	// Without this, thieves might see incremented bottom but nil task!
	atomic.AddInt64(&d.bottom, 0) // Dummy operation for fence

	// Increment bottom (relaxed - only we modify it)
	// But the fence above ensures task write happens-before this
	atomic.StoreInt64(&d.bottom, bottom+1)
}

// Pop removes and returns a task from the bottom (owner only, LIFO)
// Returns nil if empty
// NOT thread-safe - only the owning worker should call this
//
// Algorithm:
// 1. Decrement bottom
// 2. Load top
// 3. If deque is empty (top > bottom), restore bottom and return nil
// 4. Load task at bottom
// 5. If this is the last element (top == bottom), use CAS to avoid race with steal
// 6. Return task
//
// Memory Ordering:
// - SEQ_CST fence coordinates with Steal's SEQ_CST fence
// - Critical for the last-element race condition
func (d *ChaseLevDeque) Pop() func() {
	// Decrement bottom first (tentatively remove last element)
	bottom := atomic.LoadInt64(&d.bottom) - 1

	// Load current array
	array := d.array.Load().(*circularArray)

	// Store decremented bottom (relaxed)
	atomic.StoreInt64(&d.bottom, bottom)

	// CRITICAL: Full memory fence (SEQ_CST)
	// Coordinates with Steal's fence for the last-element race
	// Must happen after bottom decrement, before top load
	atomic.LoadInt64(&d.bottom) // Dummy load for SEQ_CST fence

	// Load top with relaxed ordering
	top := atomic.LoadInt64(&d.top)

	// Check if deque is empty
	if top > bottom {
		// Deque is empty - restore bottom
		atomic.StoreInt64(&d.bottom, bottom+1)
		return nil
	}

	// Get the task at bottom position
	task := array.get(bottom)

	// Check if this is the last element
	if top == bottom {
		// Last element - race condition with Steal!
		// Both Pop and Steal might try to take this element
		//
		// We use CAS to decide the winner:
		// - If CAS succeeds: we won, return the task
		// - If CAS fails: a thief stole it, return nil

		// Try to increment top from its current value
		// This prevents thieves from stealing
		if !atomic.CompareAndSwapInt64(&d.top, top, top+1) {
			// CAS failed - a thief stole the last element!
			task = nil
		}

		// Restore bottom to correct value (empty deque)
		atomic.StoreInt64(&d.bottom, bottom+1)

		return task
	}

	// Not the last element - no race possible
	// We own the bottom end exclusively
	return task
}

// Steal attempts to steal a task from the top (thieves, FIFO)
// Returns nil if empty or if lost race with another thief/owner
// Thread-safe - multiple thieves can call concurrently
//
// Algorithm:
// 1. Load top with ACQUIRE
// 2. Memory fence (SEQ_CST) - coordinate with Pop
// 3. Load bottom with ACQUIRE
// 4. Check if empty (top >= bottom)
// 5. Load task at top position
// 6. Try CAS to increment top (claim this element)
// 7. If CAS succeeds, return task; if fails, return nil
//
// Memory Ordering:
// - ACQUIRE on top/bottom ensures we see task writes
// - SEQ_CST fence coordinates with Pop's fence
// - CAS provides full barrier
func (d *ChaseLevDeque) Steal() func() {
	// Load top with ACQUIRE semantics
	// Ensures we see all writes that happened-before top was written
	top := atomic.LoadInt64(&d.top)

	// CRITICAL: Full memory fence (SEQ_CST)
	// Coordinates with Pop's fence for last-element race
	// Ensures proper ordering of top load and bottom load
	atomic.LoadInt64(&d.top) // Dummy load for SEQ_CST fence

	// Load bottom with ACQUIRE semantics
	// Ensures we see the task that owner pushed
	bottom := atomic.LoadInt64(&d.bottom)

	// Check if deque is empty
	if top >= bottom {
		return nil // Empty
	}

	// Load current array (acquire - see owner's array updates)
	array := d.array.Load().(*circularArray)

	// Get task at top position (oldest task - FIFO)
	task := array.get(top)

	// Try to claim this task by incrementing top
	// CAS handles race with other thieves and owner's Pop
	//
	// If multiple thieves try to steal simultaneously:
	// - All read same top value
	// - All try CAS with that value
	// - Only ONE succeeds
	// - Others see CAS failure and return nil
	if !atomic.CompareAndSwapInt64(&d.top, top, top+1) {
		// CAS failed - someone else (thief or owner) got this task
		return nil
	}

	// CAS succeeded - we got the task!
	return task
}

// resize creates a new larger array and copies existing elements
// Called by Push when array is full
//
// Algorithm:
// 1. Allocate new array (2x size)
// 2. Copy all elements from old array
// 3. Return new array (caller stores it atomically)
func (d *ChaseLevDeque) resize(bottom, top int64, oldArray *circularArray) *circularArray {
	// Double the capacity
	newCapacity := oldArray.capacity * 2

	// Allocate new array
	newArray := newCircularArray(newCapacity)

	// Copy all existing elements
	// From top (oldest) to bottom (newest)
	for i := top; i < bottom; i++ {
		newArray.put(i, oldArray.get(i))
	}

	return newArray
}

// Size returns an estimate of the current size
// This is a snapshot and may be stale immediately
func (d *ChaseLevDeque) Size() int64 {
	bottom := atomic.LoadInt64(&d.bottom)
	top := atomic.LoadInt64(&d.top)

	size := bottom - top

	// Clamp to 0 (shouldn't happen, but defensive)
	if size < 0 {
		return 0
	}

	return size
}

// IsEmpty returns true if the deque appears empty
// This is a snapshot and may be stale
func (d *ChaseLevDeque) IsEmpty() bool {
	return d.Size() == 0
}

// Capacity returns the current capacity
func (d *ChaseLevDeque) Capacity() int64 {
	array := d.array.Load().(*circularArray)
	return array.capacity
}

// Reset clears the deque
// NOT thread-safe - only call when no concurrent operations
func (d *ChaseLevDeque) Reset() {
	atomic.StoreInt64(&d.top, 0)
	atomic.StoreInt64(&d.bottom, 0)
	d.array.Store(newCircularArray(16))
}
