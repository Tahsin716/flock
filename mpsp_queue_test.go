package flock

import "testing"

// ============================================================================
// BASIC FUNCTIONALITY TESTS
// ============================================================================

func TestLockFreeQueue_NewPanicsOnInvalidCapacity(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic on zero capacity")
		}
	}()
	New(0)
}

func TestLockFreeQueue_NewPanicsOnNonPowerOfTwo(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic on non-power-of-2 capacity")
		}
	}()
	New(7) // Not a power of 2
}

func TestLockFreeQueue_PushPop(t *testing.T) {
	q := New(16)

	// Push a task
	executed := false
	task := func() { executed = true }

	if !q.TryPush(task) {
		t.Fatal("Failed to push to empty queue")
	}

	// Check size
	if q.Size() != 1 {
		t.Errorf("Expected size 1, got %d", q.Size())
	}

	// Pop the task
	popped := q.Pop()
	if popped == nil {
		t.Fatal("Failed to pop from queue")
	}

	// Execute and verify
	popped()
	if !executed {
		t.Error("Task was not executed")
	}

	// Check empty
	if q.Size() != 0 {
		t.Errorf("Expected size 0 after pop, got %d", q.Size())
	}
}

func TestLockFreeQueue_PopFromEmpty(t *testing.T) {
	q := New(16)

	task := q.Pop()
	if task != nil {
		t.Error("Expected nil from empty queue")
	}
}

func TestLockFreeQueue_PushNil(t *testing.T) {
	q := New(16)

	if q.TryPush(nil) {
		t.Error("Should not be able to push nil")
	}
}

func TestLockFreeQueue_FillAndDrain(t *testing.T) {
	capacity := 16
	q := New(capacity)

	// Fill queue (capacity - 1, as we leave one slot empty)
	for i := 0; i < capacity-1; i++ {
		task := func() {}
		if !q.TryPush(task) {
			t.Fatalf("Failed to push at index %d", i)
		}
	}

	// Check size
	if q.Size() != capacity-1 {
		t.Errorf("Expected size %d, got %d", capacity-1, q.Size())
	}

	// Should be full now
	if q.TryPush(func() {}) {
		t.Error("Should not be able to push to full queue")
	}

	// Drain all
	for i := 0; i < capacity-1; i++ {
		task := q.Pop()
		if task == nil {
			t.Fatalf("Failed to pop at index %d", i)
		}
	}

	// Should be empty
	if q.Pop() != nil {
		t.Error("Expected nil from empty queue")
	}
}

func TestLockFreeQueue_WrapAround(t *testing.T) {
	q := New(8)

	// Push and pop multiple times to test wrap-around
	for i := 0; i < 100; i++ {
		count := 0
		task := func() { count++ }

		if !q.TryPush(task) {
			t.Fatalf("Failed to push at iteration %d", i)
		}

		popped := q.Pop()
		if popped == nil {
			t.Fatalf("Failed to pop at iteration %d", i)
		}

		popped()
		if count != 1 {
			t.Errorf("Task not executed correctly at iteration %d", i)
		}
	}
}
