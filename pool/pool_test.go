package pool

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// Core Pool Tests
// ============================================================================

func TestPoolCreation(t *testing.T) {
	pool := New(10)
	defer pool.Close()

	if pool.Cap() != 10 {
		t.Errorf("Expected capacity 10, got %d", pool.Cap())
	}
}

func TestPoolDefaultCapacity(t *testing.T) {
	pool := New(0)
	defer pool.Close()

	expected := runtime.GOMAXPROCS(0)
	if pool.Cap() != expected {
		t.Errorf("Expected default capacity %d, got %d", expected, pool.Cap())
	}
}

func TestPoolGo(t *testing.T) {
	pool := New(5)
	defer pool.Close()

	var counter int32
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		err := pool.Go(func() {
			atomic.AddInt32(&counter, 1)
			wg.Done()
		})
		if err != nil {
			t.Errorf("Go failed: %v", err)
		}
	}

	wg.Wait()

	if counter != 10 {
		t.Errorf("Expected counter=10, got %d", counter)
	}
}

func TestPoolTryGo(t *testing.T) {
	pool := New(2)
	defer pool.Close()

	// Fill pool
	var blocker sync.WaitGroup
	blocker.Add(2)

	pool.Go(func() {
		blocker.Wait()
	})
	pool.Go(func() {
		blocker.Wait()
	})

	// Give time for tasks to start
	time.Sleep(10 * time.Millisecond)

	// TryGo should fail (pool full)
	success := pool.TryGo(func() {})
	if success {
		t.Error("TryGo should fail when pool is full")
	}

	blocker.Done()
	blocker.Done()

	// Wait for tasks to complete
	pool.Wait()

	// Now TryGo should succeed
	success = pool.TryGo(func() {})
	if !success {
		t.Error("TryGo should succeed when pool has space")
	}

	pool.Wait()
}

func TestPoolNilTask(t *testing.T) {
	pool := New(5)
	defer pool.Close()

	err := pool.Go(nil)
	if err != ErrTaskNil {
		t.Errorf("Expected ErrTaskNil, got %v", err)
	}
}

func TestPoolWait(t *testing.T) {
	pool := New(5)
	defer pool.Close()

	var counter int32

	for i := 0; i < 20; i++ {
		pool.Go(func() {
			time.Sleep(10 * time.Millisecond)
			atomic.AddInt32(&counter, 1)
		})
	}

	pool.Wait()

	if counter != 20 {
		t.Errorf("Expected all 20 tasks completed, got %d", counter)
	}
}

func TestPoolStats(t *testing.T) {
	pool := New(5)
	defer pool.Close()

	for i := 0; i < 10; i++ {
		pool.Go(func() {
			time.Sleep(time.Millisecond)
		})
	}

	pool.Wait()

	submitted, completed := pool.Stats()

	if submitted != 10 {
		t.Errorf("Expected 10 submitted, got %d", submitted)
	}

	if completed != 10 {
		t.Errorf("Expected 10 completed, got %d", completed)
	}
}

func TestPoolClose(t *testing.T) {
	pool := New(5)
	pool.Close()

	err := pool.Go(func() {})
	if err != ErrPoolClosed {
		t.Errorf("Expected ErrPoolClosed, got %v", err)
	}
}

func TestPoolPanicRecovery(t *testing.T) {
	pool := New(5)
	defer pool.Close()

	// Submit task that panics
	pool.Go(func() {
		panic("test panic")
	})

	// Pool should still work
	var wg sync.WaitGroup
	wg.Add(1)
	err := pool.Go(func() {
		wg.Done()
	})

	if err != nil {
		t.Errorf("Pool should work after panic: %v", err)
	}

	wg.Wait()
}

func TestPoolConcurrentSubmit(t *testing.T) {
	pool := New(10)
	defer pool.Close()

	const goroutines = 50
	const tasksPerGoroutine = 10

	var counter int32
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < tasksPerGoroutine; j++ {
				pool.Go(func() {
					atomic.AddInt32(&counter, 1)
				})
			}
		}()
	}

	wg.Wait()
	pool.Wait()

	expected := int32(goroutines * tasksPerGoroutine)
	if counter != expected {
		t.Errorf("Expected counter=%d, got %d", expected, counter)
	}
}
