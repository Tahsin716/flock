package pool

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewFastPool(t *testing.T) {
	t.Run("DefaultSize", func(t *testing.T) {
		p := NewFast(0)
		defer p.Close()
		if p.Cap() != runtime.GOMAXPROCS(0) {
			t.Errorf("Expected default size to be %d, got %d", runtime.GOMAXPROCS(0), p.Cap())
		}
	})

	t.Run("CustomSize", func(t *testing.T) {
		p := NewFast(10)
		defer p.Close()
		if p.Cap() != 10 {
			t.Errorf("Expected custom size to be 10, got %d", p.Cap())
		}
	})
}

func TestFastPoolSubmit(t *testing.T) {
	p := NewFast(2)

	var counter int64
	var wg sync.WaitGroup
	numTasks := 10
	wg.Add(numTasks)

	task := func() {
		atomic.AddInt64(&counter, 1)
		wg.Done()
	}

	for i := 0; i < numTasks; i++ {
		p.Submit(task)
	}

	p.Close() // Close will block until all tasks are done.

	if atomic.LoadInt64(&counter) != int64(numTasks) {
		t.Errorf("Expected counter to be %d, got %d", numTasks, counter)
	}

	stats := p.Stats()
	if stats.Submitted != int64(numTasks) || stats.Completed != int64(numTasks) || stats.Failed != 0 {
		t.Errorf("Stats are incorrect, got %+v", stats)
	}
}

func TestFastPoolTrySubmit(t *testing.T) {
	// Create a pool with 1 worker and a queue buffer of size 1.
	// This means it can have 1 job running and 1 job waiting in the queue.
	p := NewFast(1)
	defer p.Close()

	// startSignal is used by the test to tell the first task when to start its "work".
	// This allows us to pause the worker in a known state.
	startSignal := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(1)

	// This task will be picked up by the worker but will block until we signal it.
	blockingTask := func() {
		// Wait for the signal from the main test goroutine.
		<-startSignal
		// Do a little work after being unblocked.
		time.Sleep(10 * time.Millisecond)
		wg.Done()
	}

	p.Submit(blockingTask)

	p.Submit(func() {})

	if p.TrySubmit(func() {}) {
		t.Error("TrySubmit should have failed on a fully saturated pool")
	}

	close(startSignal)
	wg.Wait()

	// Give the scheduler a moment to let the second task (the empty func) run.
	time.Sleep(10 * time.Millisecond)

	if !p.TrySubmit(func() {}) {
		t.Error("TrySubmit should have succeeded after the pool became free")
	}
}

func TestFastPoolPanicHandling(t *testing.T) {
	p := NewFast(1)

	panicTask := func() {
		panic("fast pool test panic")
	}

	p.Submit(panicTask)
	p.Close()

	stats := p.Stats()
	if stats.Failed != 1 || stats.Completed != 0 {
		t.Errorf("Expected 1 failed task after a panic, got %+v", stats)
	}
}

func TestFastPoolClose(t *testing.T) {
	p := NewFast(4)
	var completed int64

	task := func() {
		time.Sleep(10 * time.Millisecond)
		atomic.AddInt64(&completed, 1)
	}

	for i := 0; i < 20; i++ {
		p.Submit(task)
	}

	p.Close() // Should block until all 20 tasks are drained and executed.

	if atomic.LoadInt64(&completed) != 20 {
		t.Errorf("Expected 20 completed tasks, got %d", atomic.LoadInt64(&completed))
	}

	// Submitting to a closed pool should be a no-op
	p.Submit(task)
	if p.Stats().Submitted != 20 {
		t.Errorf("Task should not be submitted to a closed pool")
	}

	// TrySubmit on a closed pool should also be a no-op and return false
	if p.TrySubmit(task) {
		t.Error("TrySubmit should return false for a closed pool")
	}
	if p.Stats().Submitted != 20 {
		t.Errorf("Task should not be submitted to a closed pool via TrySubmit")
	}
}

func TestFastPoolConcurrency(t *testing.T) {
	p := NewFast(64)
	defer p.Close()

	numGoroutines := 100
	tasksPerGoroutine := 20
	totalTasks := numGoroutines * tasksPerGoroutine
	var counter int64

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < tasksPerGoroutine; j++ {
				p.Submit(func() {
					atomic.AddInt64(&counter, 1)
					time.Sleep(time.Microsecond)
				})
			}
		}()
	}

	wg.Wait()
	p.Close()

	if atomic.LoadInt64(&counter) != int64(totalTasks) {
		t.Errorf("Expected counter to be %d, got %d", totalTasks, counter)
	}
	stats := p.Stats()
	if stats.Submitted != int64(totalTasks) || stats.Completed != int64(totalTasks) {
		t.Errorf("Stats are incorrect, got %+v", stats)
	}
}
