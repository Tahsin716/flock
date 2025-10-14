package flock

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// BASIC FUNCTIONALITY TESTS
// ============================================================================

func TestLockFreeQueue_NewPanicsOnInvalidCapacity(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic on zero capacity")
		}
	}()
	newQueue(0)
}

func TestLockFreeQueue_NewPanicsOnNonPowerOfTwo(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic on non-power-of-2 capacity")
		}
	}()
	newQueue(7) // Not a power of 2
}

func TestLockFreeQueue_PushPop(t *testing.T) {
	q := newQueue(16)

	// Push a task
	executed := false
	task := func() { executed = true }

	if !q.tryPush(task) {
		t.Fatal("Failed to push to empty queue")
	}

	// Check size
	if q.size() != 1 {
		t.Errorf("Expected size 1, got %d", q.size())
	}

	// pop the task
	popped := q.pop()
	if popped == nil {
		t.Fatal("Failed to pop from queue")
	}

	// Execute and verify
	popped()
	if !executed {
		t.Error("Task was not executed")
	}

	// Check empty
	if q.size() != 0 {
		t.Errorf("Expected size 0 after pop, got %d", q.size())
	}
}

func TestLockFreeQueue_PopFromEmpty(t *testing.T) {
	q := newQueue(16)

	task := q.pop()
	if task != nil {
		t.Error("Expected nil from empty queue")
	}
}

func TestLockFreeQueue_PushNil(t *testing.T) {
	q := newQueue(16)

	if q.tryPush(nil) {
		t.Error("Should not be able to push nil")
	}
}

func TestLockFreeQueue_FillAndDrain(t *testing.T) {
	capacity := 16
	q := newQueue(capacity)

	// Fill queue (capacity - 1, as we leave one slot empty)
	for i := 0; i < capacity-1; i++ {
		task := func() {}
		if !q.tryPush(task) {
			t.Fatalf("Failed to push at index %d", i)
		}
	}

	// Check size
	if q.size() != capacity-1 {
		t.Errorf("Expected size %d, got %d", capacity-1, q.size())
	}

	// Should be full now
	if q.tryPush(func() {}) {
		t.Error("Should not be able to push to full queue")
	}

	// Drain all
	for i := 0; i < capacity-1; i++ {
		task := q.pop()
		if task == nil {
			t.Fatalf("Failed to pop at index %d", i)
		}
	}

	// Should be empty
	if q.pop() != nil {
		t.Error("Expected nil from empty queue")
	}
}

func TestLockFreeQueue_WrapAround(t *testing.T) {
	q := newQueue(8)

	// Push and pop multiple times to test wrap-around
	for i := 0; i < 100; i++ {
		count := 0
		task := func() { count++ }

		if !q.tryPush(task) {
			t.Fatalf("Failed to push at iteration %d", i)
		}

		popped := q.pop()
		if popped == nil {
			t.Fatalf("Failed to pop at iteration %d", i)
		}

		popped()
		if count != 1 {
			t.Errorf("Task not executed correctly at iteration %d", i)
		}
	}
}

// ============================================================================
// CONCURRENT TESTS
// ============================================================================

func TestLockFreeQueue_ConcurrentPushPop(t *testing.T) {
	q := newQueue(1024)
	numProducers := 4
	itemsPerProducer := 10000

	var pushed, popped int64
	var wg sync.WaitGroup

	// Start producers
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < itemsPerProducer; j++ {
				// Keep trying until successful
				for !q.tryPush(func() {}) {
					runtime.Gosched()
				}
				atomic.AddInt64(&pushed, 1)
			}
		}(i)
	}

	// Start consumer
	wg.Add(1)
	go func() {
		defer wg.Done()
		expected := int64(numProducers * itemsPerProducer)
		for atomic.LoadInt64(&popped) < expected {
			if task := q.pop(); task != nil {
				atomic.AddInt64(&popped, 1)
				task()
			} else {
				runtime.Gosched()
			}
		}
	}()

	wg.Wait()

	if pushed != popped {
		t.Errorf("Lost tasks: pushed %d, popped %d", pushed, popped)
	}

	if !q.isEmpty() {
		t.Errorf("Queue not empty after test, size: %d", q.size())
	}
}

func TestLockFreeQueue_HighContention(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping high contention test in short mode")
	}

	q := newQueue(2048)
	numProducers := runtime.NumCPU()
	itemsPerProducer := 50000

	var pushed, popped int64
	var wg sync.WaitGroup
	var producersDone int32

	// Many producers
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < itemsPerProducer; j++ {
				for !q.tryPush(func() {}) {
					runtime.Gosched()
				}
				atomic.AddInt64(&pushed, 1)
			}
		}()
	}

	// Signal when all producers are done
	go func() {
		wg.Wait()
		atomic.StoreInt32(&producersDone, 1)
	}()

	// Single consumer
	expected := int64(numProducers * itemsPerProducer)
	for {
		if task := q.pop(); task != nil {
			atomic.AddInt64(&popped, 1)
			// Check if we've processed all items
			if atomic.LoadInt64(&popped) >= expected {
				break
			}
		} else {
			// Queue is empty - check if producers are done
			if atomic.LoadInt32(&producersDone) == 1 {
				// Producers done, but double-check queue is empty
				if q.pop() == nil {
					break
				}
			} else {
				// Producers still working, yield and retry
				runtime.Gosched()
			}
		}
	}

	// Wait for all producers to finish
	wg.Wait()

	if pushed != popped {
		t.Errorf("Lost tasks under high contention: pushed %d, popped %d", pushed, popped)
	}
}

// ============================================================================
// STRESS TESTS
// ============================================================================

func TestLockFreeQueue_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	q := newQueue(2048)
	duration := 3 * time.Second

	var pushAttempts, pushSuccesses int64
	var popAttempts, popSuccesses int64
	stop := make(chan struct{})

	// Producers
	numProducers := 4
	for i := 0; i < numProducers; i++ {
		go func() {
			localPushes := int64(0)
			for {
				select {
				case <-stop:
					atomic.AddInt64(&pushSuccesses, localPushes)
					return
				default:
					atomic.AddInt64(&pushAttempts, 1)
					if q.tryPush(func() {}) {
						localPushes++
					}
				}
			}
		}()
	}

	// Consumer
	go func() {
		localPops := int64(0)
		for {
			select {
			case <-stop:
				// Drain remaining
				for q.pop() != nil {
					localPops++
				}
				atomic.AddInt64(&popSuccesses, localPops)
				return
			default:
				atomic.AddInt64(&popAttempts, 1)
				if task := q.pop(); task != nil {
					localPops++
				}
			}
		}
	}()

	// Run for duration
	time.Sleep(duration)
	close(stop)

	// Give time to drain and collect final counts
	time.Sleep(500 * time.Millisecond)

	finalPushed := atomic.LoadInt64(&pushSuccesses)
	finalPopped := atomic.LoadInt64(&popSuccesses)

	// Final queue check
	remaining := q.size()

	if finalPushed != finalPopped+int64(remaining) {
		t.Errorf("Stress test failed: pushed %d, popped %d, remaining %d (expected: pushed = popped + remaining)",
			finalPushed, finalPopped, remaining)
	}

	t.Logf("Stress test completed: %d tasks processed in %v", finalPopped, duration)
}
