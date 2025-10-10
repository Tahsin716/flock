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

func TestChaseLevDeque_PushPop(t *testing.T) {
	d := newChaseLevDeque(16)

	// push a task
	executed := false
	task := func() { executed = true }

	d.push(task)

	if d.size() != 1 {
		t.Errorf("Expected size 1, got %d", d.size())
	}

	// pop the task (LIFO - should get the same task)
	popped := d.pop()
	if popped == nil {
		t.Fatal("Failed to pop from deque")
	}

	popped()
	if !executed {
		t.Error("Task was not executed")
	}

	if d.size() != 0 {
		t.Errorf("Expected size 0 after pop, got %d", d.size())
	}
}

func TestChaseLevDeque_PopFromEmpty(t *testing.T) {
	d := newChaseLevDeque(16)

	task := d.pop()
	if task != nil {
		t.Error("Expected nil from empty deque")
	}
}

func TestChaseLevDeque_StealFromEmpty(t *testing.T) {
	d := newChaseLevDeque(16)

	task := d.steal()
	if task != nil {
		t.Error("Expected nil when stealing from empty deque")
	}
}

func TestChaseLevDeque_PushNil(t *testing.T) {
	d := newChaseLevDeque(16)
	d.push(nil) // Should not panic, just ignore

	if d.size() != 0 {
		t.Error("Pushing nil should not add to size")
	}
}

func TestChaseLevDeque_LIFO_Order(t *testing.T) {
	d := newChaseLevDeque(16)

	// push tasks in order
	ids := []int{}
	for i := 0; i < 5; i++ {
		id := i
		task := func() { ids = append(ids, id) }
		d.push(task)
	}

	// pop should return in LIFO order (4, 3, 2, 1, 0)
	for i := 4; i >= 0; i-- {
		task := d.pop()
		if task == nil {
			t.Fatalf("Failed to pop task at position %d", i)
		}
		task()
	}

	// Verify LIFO order
	expected := []int{4, 3, 2, 1, 0}
	if len(ids) != len(expected) {
		t.Fatalf("Expected %d tasks, got %d", len(expected), len(ids))
	}

	for i, id := range ids {
		if id != expected[i] {
			t.Errorf("Expected id %d at position %d, got %d", expected[i], i, id)
		}
	}
}

func TestChaseLevDeque_FIFO_StealOrder(t *testing.T) {
	d := newChaseLevDeque(16)

	// push tasks in order
	ids := []int{}
	for i := 0; i < 5; i++ {
		id := i
		task := func() { ids = append(ids, id) }
		d.push(task)
	}

	// steal should return in FIFO order (0, 1, 2, 3, 4)
	for i := 0; i < 5; i++ {
		task := d.steal()
		if task == nil {
			t.Fatalf("Failed to steal task at position %d", i)
		}
		task()
	}

	// Verify FIFO order
	expected := []int{0, 1, 2, 3, 4}
	if len(ids) != len(expected) {
		t.Fatalf("Expected %d tasks, got %d", len(expected), len(ids))
	}

	for i, id := range ids {
		if id != expected[i] {
			t.Errorf("Expected id %d at position %d, got %d", expected[i], i, id)
		}
	}
}

func TestChaseLevDeque_Resize(t *testing.T) {
	d := newChaseLevDeque(4) // Small initial capacity

	initialCap := d.capacity()
	if initialCap != 4 {
		t.Errorf("Expected initial capacity 4, got %d", initialCap)
	}

	// push more than initial capacity to trigger resize
	for i := 0; i < 10; i++ {
		d.push(func() {})
	}

	newCap := d.capacity()
	if newCap <= initialCap {
		t.Errorf("Expected capacity to increase from %d, got %d", initialCap, newCap)
	}

	// Should be able to pop all items
	for i := 0; i < 10; i++ {
		task := d.pop()
		if task == nil {
			t.Fatalf("Failed to pop after resize at index %d", i)
		}
	}
}

// ============================================================================
// CONCURRENT TESTS - Owner vs Thieves
// ============================================================================

func TestChaseLevDeque_PopAndStealLastElement(t *testing.T) {
	// This tests the critical race condition when only one element remains
	// Both pop and steal try to take it - only one should succeed

	const iterations = 10000

	for iter := 0; iter < iterations; iter++ {
		d := newChaseLevDeque(16)

		// push one task
		d.push(func() {})

		var popGot, stealGot int32
		var wg sync.WaitGroup

		// Owner tries to pop
		wg.Add(1)
		go func() {
			defer wg.Done()
			if d.pop() != nil {
				atomic.StoreInt32(&popGot, 1)
			}
		}()

		// Thief tries to steal
		wg.Add(1)
		go func() {
			defer wg.Done()
			if d.steal() != nil {
				atomic.StoreInt32(&stealGot, 1)
			}
		}()

		wg.Wait()

		// Exactly one should have gotten the task
		total := atomic.LoadInt32(&popGot) + atomic.LoadInt32(&stealGot)
		if total != 1 {
			t.Errorf("Iteration %d: Expected exactly 1 to get the task, got %d (pop:%d, steal:%d)",
				iter, total, popGot, stealGot)
		}
	}
}

func TestChaseLevDeque_MultipleThieves(t *testing.T) {
	d := newChaseLevDeque(16)

	// Owner pushes many tasks
	const numTasks = 1000
	for i := 0; i < numTasks; i++ {
		d.push(func() {})
	}

	// Multiple thieves try to steal
	const numThieves = 4
	var stolen [numThieves]int64
	var wg sync.WaitGroup

	for i := 0; i < numThieves; i++ {
		wg.Add(1)
		thiefID := i
		go func() {
			defer wg.Done()
			for {
				task := d.steal()
				if task == nil {
					break
				}
				atomic.AddInt64(&stolen[thiefID], 1)
			}
		}()
	}

	wg.Wait()

	// Sum up all stolen tasks
	totalStolen := int64(0)
	for i := 0; i < numThieves; i++ {
		totalStolen += stolen[i]
	}

	if totalStolen != numTasks {
		t.Errorf("Expected %d tasks stolen, got %d", numTasks, totalStolen)
	}

	// Deque should be empty
	if !d.isEmpty() {
		t.Errorf("Deque should be empty, size: %d", d.size())
	}
}

func TestChaseLevDeque_OwnerPushPopThievesSteal(t *testing.T) {
	d := newChaseLevDeque(128)
	duration := 2 * time.Second

	var pushed, ownerPopped, stolen int64
	stop := make(chan struct{})

	// Owner: continuously push and pop
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				// push some tasks
				for i := 0; i < 10; i++ {
					d.push(func() {})
					atomic.AddInt64(&pushed, 1)
				}

				// pop some tasks
				for i := 0; i < 5; i++ {
					if d.pop() != nil {
						atomic.AddInt64(&ownerPopped, 1)
					}
				}
			}
		}
	}()

	// Thieves: continuously steal
	numThieves := 3
	for i := 0; i < numThieves; i++ {
		go func() {
			for {
				select {
				case <-stop:
					return
				default:
					if d.steal() != nil {
						atomic.AddInt64(&stolen, 1)
					} else {
						runtime.Gosched()
					}
				}
			}
		}()
	}

	// Run for duration
	time.Sleep(duration)
	close(stop)

	// Drain remaining
	time.Sleep(100 * time.Millisecond)
	for d.pop() != nil {
		atomic.AddInt64(&ownerPopped, 1)
	}

	finalPushed := atomic.LoadInt64(&pushed)
	finalOwnerPopped := atomic.LoadInt64(&ownerPopped)
	finalStolen := atomic.LoadInt64(&stolen)

	total := finalOwnerPopped + finalStolen

	if total != finalPushed {
		t.Errorf("Task mismatch: pushed %d, owner popped %d, stolen %d, total %d",
			finalPushed, finalOwnerPopped, finalStolen, total)
	}

	t.Logf("Concurrent test: pushed %d, owner popped %d, stolen %d",
		finalPushed, finalOwnerPopped, finalStolen)
}

// Test: Verify no duplicate task execution
// Each task has a unique ID, ensure each ID is executed exactly once
func TestChaseLevDeque_NoDuplicates(t *testing.T) {
	d := newChaseLevDeque(128)
	const numTasks = 10000

	// Track which IDs were executed
	executed := make(map[int]*int32)
	executedMu := sync.Mutex{}

	// push tasks with unique IDs
	for i := 0; i < numTasks; i++ {
		id := i
		counter := new(int32)
		executed[id] = counter

		task := func() {
			atomic.AddInt32(counter, 1)
		}
		d.push(task)
	}

	// Multiple thieves steal and execute
	var wg sync.WaitGroup
	numThieves := 4

	for i := 0; i < numThieves; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				task := d.steal()
				if task == nil {
					time.Sleep(time.Microsecond)
					// Double-check it's really empty
					if d.isEmpty() {
						break
					}
					continue
				}
				task()
			}
		}()
	}

	// Owner also pops and executes
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			task := d.pop()
			if task == nil {
				if d.isEmpty() {
					break
				}
				continue
			}
			task()
		}
	}()

	wg.Wait()

	// Verify each task executed exactly once
	executedMu.Lock()
	defer executedMu.Unlock()

	for id := 0; id < numTasks; id++ {
		count := atomic.LoadInt32(executed[id])
		if count != 1 {
			t.Errorf("Task %d executed %d times (expected 1)", id, count)
		}
	}
}

// Test: Verify memory ordering with data dependency
// Tasks write to shared memory; ensure no data races or stale reads
func TestChaseLevDeque_MemoryOrdering(t *testing.T) {
	d := newChaseLevDeque(64)
	const numPairs = 5000

	// Each task pair: writer task sets value, reader task checks it
	errors := new(int32)

	for i := 0; i < numPairs; i++ {
		value := new(int32)
		expected := int32(i + 42)

		// Writer task
		writerTask := func() {
			atomic.StoreInt32(value, expected)
		}

		// Reader task (must see writer's value)
		readerTask := func() {
			seen := atomic.LoadInt32(value)
			if seen != 0 && seen != expected {
				atomic.AddInt32(errors, 1)
			}
		}

		d.push(writerTask)
		d.push(readerTask)
	}

	// Execute all tasks
	var wg sync.WaitGroup
	numWorkers := 4

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				task := d.steal()
				if task == nil {
					if d.isEmpty() {
						break
					}
					runtime.Gosched()
					continue
				}
				task()
			}
		}()
	}

	wg.Wait()

	if errorCount := atomic.LoadInt32(errors); errorCount > 0 {
		t.Errorf("Memory ordering errors detected: %d", errorCount)
	}
}

// Test: Rapid resize under concurrent access
func TestChaseLevDeque_ResizeUnderLoad(t *testing.T) {
	d := newChaseLevDeque(2) // Very small initial capacity
	duration := 1 * time.Second

	var pushed, consumed int64
	stop := make(chan struct{})

	// Owner: rapidly push to force many resizes
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				// push bursts to trigger resize
				for i := 0; i < 20; i++ {
					d.push(func() {})
					atomic.AddInt64(&pushed, 1)
				}
				// pop a few to allow more pushing
				for i := 0; i < 5; i++ {
					if d.pop() != nil {
						atomic.AddInt64(&consumed, 1)
					}
				}
			}
		}
	}()

	// Thieves steal during resizes
	numThieves := 3
	for i := 0; i < numThieves; i++ {
		go func() {
			for {
				select {
				case <-stop:
					return
				default:
					if d.steal() != nil {
						atomic.AddInt64(&consumed, 1)
					}
				}
			}
		}()
	}

	time.Sleep(duration)
	close(stop)
	time.Sleep(50 * time.Millisecond)

	// Drain remaining
	for d.pop() != nil {
		atomic.AddInt64(&consumed, 1)
	}

	finalPushed := atomic.LoadInt64(&pushed)
	finalConsumed := atomic.LoadInt64(&consumed)

	if finalPushed != finalConsumed {
		t.Errorf("Resize test: pushed %d, consumed %d", finalPushed, finalConsumed)
	}

	t.Logf("Resize test: %d tasks, final capacity %d", finalPushed, d.capacity())
}

// Test: Empty deque race - multiple operations on empty deque
func TestChaseLevDeque_EmptyDequeRace(t *testing.T) {
	const iterations = 1000

	for iter := 0; iter < iterations; iter++ {
		d := newChaseLevDeque(16)

		var wg sync.WaitGroup

		// Multiple thieves try to steal from empty deque
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					d.steal()
				}
			}()
		}

		// Owner tries to pop from empty deque
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				d.pop()
			}
		}()

		wg.Wait()

		// Should still be empty and not panic
		if !d.isEmpty() {
			t.Errorf("Iteration %d: deque should be empty", iter)
		}
	}
}

// Test: Single element race - push one, many try to get it
func TestChaseLevDeque_SingleElementContention(t *testing.T) {
	const iterations = 5000

	for iter := 0; iter < iterations; iter++ {
		d := newChaseLevDeque(16)

		// push exactly one task
		d.push(func() {})

		var gotTask int32
		var wg sync.WaitGroup

		// Owner tries to pop
		wg.Add(1)
		go func() {
			defer wg.Done()
			if d.pop() != nil {
				atomic.AddInt32(&gotTask, 1)
			}
		}()

		// Multiple thieves try to steal
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if d.steal() != nil {
					atomic.AddInt32(&gotTask, 1)
				}
			}()
		}

		wg.Wait()

		// Exactly one should have gotten it
		count := atomic.LoadInt32(&gotTask)
		if count != 1 {
			t.Errorf("Iteration %d: Expected 1 to get task, got %d", iter, count)
		}

		// Deque should be empty
		if !d.isEmpty() {
			t.Errorf("Iteration %d: deque should be empty", iter)
		}
	}
}

// Test: Alternating push/pop/steal pattern
func TestChaseLevDeque_AlternatingPattern(t *testing.T) {
	d := newChaseLevDeque(32)
	const cycles = 1000

	var pushed, ownerGot, thiefGot int64

	for i := 0; i < cycles; i++ {
		// push 10
		for j := 0; j < 10; j++ {
			d.push(func() {})
			atomic.AddInt64(&pushed, 1)
		}

		var wg sync.WaitGroup

		// Owner pops 5
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if d.pop() != nil {
					atomic.AddInt64(&ownerGot, 1)
				}
			}
		}()

		// Thief steals 5
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if d.steal() != nil {
					atomic.AddInt64(&thiefGot, 1)
				}
			}
		}()

		wg.Wait()
	}

	// Drain remaining
	for d.pop() != nil {
		atomic.AddInt64(&ownerGot, 1)
	}

	total := atomic.LoadInt64(&ownerGot) + atomic.LoadInt64(&thiefGot)
	if total != atomic.LoadInt64(&pushed) {
		t.Errorf("Mismatch: pushed %d, got %d", pushed, total)
	}
}

// ============================================================================
// STRESS TESTS
// ============================================================================

func TestChaseLevDeque_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	d := newChaseLevDeque(256)
	duration := 3 * time.Second

	var pushed, popped, stolen int64
	stop := make(chan struct{})

	// Owner: push and pop aggressively
	go func() {
		for {
			select {
			case <-stop:
				// Drain
				for d.pop() != nil {
					atomic.AddInt64(&popped, 1)
				}
				return
			default:
				// push burst
				for i := 0; i < 50; i++ {
					d.push(func() {})
					atomic.AddInt64(&pushed, 1)
				}

				// pop some
				for i := 0; i < 25; i++ {
					if d.pop() != nil {
						atomic.AddInt64(&popped, 1)
					}
				}
			}
		}
	}()

	// Multiple thieves
	numThieves := runtime.NumCPU()
	for i := 0; i < numThieves; i++ {
		go func() {
			for {
				select {
				case <-stop:
					return
				default:
					if d.steal() != nil {
						atomic.AddInt64(&stolen, 1)
					}
				}
			}
		}()
	}

	time.Sleep(duration)
	close(stop)
	time.Sleep(100 * time.Millisecond)

	finalPushed := atomic.LoadInt64(&pushed)
	finalPopped := atomic.LoadInt64(&popped)
	finalStolen := atomic.LoadInt64(&stolen)
	total := finalPopped + finalStolen

	if total != finalPushed {
		t.Errorf("Stress test failed: pushed %d, popped %d, stolen %d, total %d",
			finalPushed, finalPopped, finalStolen, total)
	}

	t.Logf("Stress test: %d tasks processed (%d popped, %d stolen) in %v",
		total, finalPopped, finalStolen, duration)
}
