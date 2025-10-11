package flock

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// Pool Creation Tests
// ============================================================================

func TestNewPool_DefaultConfig(t *testing.T) {
	pool, err := NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	if pool.NumWorkers() != runtime.NumCPU() {
		t.Errorf("Expected %d workers, got %d", runtime.NumCPU(), pool.NumWorkers())
	}
}

func TestNewPool_WithOptions(t *testing.T) {
	pool, err := NewPool(
		WithNumWorkers(4),
		WithQueueSizePerWorker(128),
		WithSpinCount(10),
		WithMaxParkTime(5*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	if pool.NumWorkers() != 4 {
		t.Errorf("Expected 4 workers, got %d", pool.NumWorkers())
	}
}

func TestNewPool_InvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
	}{
		{
			name: "negative workers",
			opts: []Option{WithNumWorkers(-1)},
		},
		{
			name: "zero queue size",
			opts: []Option{WithQueueSizePerWorker(0)},
		},
		{
			name: "non-power-of-2 queue",
			opts: []Option{WithQueueSizePerWorker(100)},
		},
		{
			name: "negative spin count",
			opts: []Option{WithSpinCount(-1)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPool(tt.opts...)
			if err == nil {
				t.Error("Expected error, got nil")
			}
		})
	}
}

// ============================================================================
// Submit Tests
// ============================================================================

func TestPool_Submit_Success(t *testing.T) {
	pool, err := NewPool(WithNumWorkers(2))
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	var executed atomic.Int32
	err = pool.Submit(func() {
		executed.Add(1)
	})

	if err != nil {
		t.Errorf("Submit() error = %v", err)
	}

	pool.Wait()

	if executed.Load() != 1 {
		t.Errorf("Expected 1 execution, got %d", executed.Load())
	}
}

func TestPool_Submit_NilTask(t *testing.T) {
	pool, err := NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	err = pool.Submit(nil)
	if !errors.Is(err, ErrNilTask) {
		t.Errorf("Expected ErrNilTask, got %v", err)
	}
}

func TestPool_Submit_AfterShutdown(t *testing.T) {
	pool, err := NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	pool.Shutdown(false)

	err = pool.Submit(func() {})
	if !errors.Is(err, ErrPoolShutdown) {
		t.Errorf("Expected ErrPoolShutdown, got %v", err)
	}
}

func TestPool_Submit_Concurrent(t *testing.T) {
	pool, err := NewPool(WithNumWorkers(4))
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	const numTasks = 1000
	var completed atomic.Int32

	var wg sync.WaitGroup
	for i := 0; i < numTasks; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool.Submit(func() {
				completed.Add(1)
				time.Sleep(time.Microsecond)
			})
		}()
	}

	wg.Wait()
	pool.Wait()

	if completed.Load() != numTasks {
		t.Errorf("Expected %d completions, got %d", numTasks, completed.Load())
	}
}

func TestPool_Submit_FallbackExecution(t *testing.T) {
	pool, _ := NewPool(
		WithNumWorkers(1),
		WithQueueSizePerWorker(2),
	)
	defer pool.Shutdown(false)

	// Fill queue with blocking tasks
	blockChan := make(chan struct{})
	for i := 0; i < 10; i++ {
		pool.Submit(func() {
			<-blockChan
		})
	}

	time.Sleep(50 * time.Millisecond)

	// Submit NON-BLOCKING task for fallback
	var executed atomic.Bool
	pool.Submit(func() {
		executed.Store(true) // Fast, non-blocking
	})

	//  Now we can unblock workers
	close(blockChan)
	pool.Wait()

	// Verify fallback happened
	if !executed.Load() {
		t.Error("Fallback task not executed")
	}

	stats := pool.Stats()
	if stats.FallbackExecuted == 0 {
		t.Error("Expected fallback execution")
	}
}

// ============================================================================
// Panic Handling Tests
// ============================================================================

func TestPool_PanicRecovery_DefaultHandler(t *testing.T) {
	pool, err := NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	pool.Submit(func() {
		panic("test panic")
	})

	pool.Wait()

	// Pool should still be functional
	var executed atomic.Bool
	pool.Submit(func() {
		executed.Store(true)
	})
	pool.Wait()

	if !executed.Load() {
		t.Error("Pool should still work after panic")
	}
}

func TestPool_PanicRecovery_CustomHandler(t *testing.T) {
	var panicValue atomic.Value
	pool, err := NewPool(
		WithPanicHandler(func(r interface{}) {
			panicValue.Store(r)
		}),
	)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	pool.Submit(func() {
		panic("custom panic")
	})

	pool.Wait()

	recovered := panicValue.Load()
	if recovered == nil {
		t.Error("Panic handler was not called")
	}
	if str, ok := recovered.(string); !ok || str != "custom panic" {
		t.Errorf("Expected 'custom panic', got %v", recovered)
	}
}

// ============================================================================
// Shutdown Tests
// ============================================================================

func TestPool_Shutdown_Graceful(t *testing.T) {
	pool, err := NewPool(WithNumWorkers(2))
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}

	var completed atomic.Int32
	for i := 0; i < 100; i++ {
		pool.Submit(func() {
			time.Sleep(time.Millisecond)
			completed.Add(1)
		})
	}

	// Graceful shutdown waits for tasks
	pool.Shutdown(true)

	if completed.Load() != 100 {
		t.Errorf("Expected 100 completions, got %d", completed.Load())
	}

	if !pool.IsShutdown() {
		t.Error("Pool should be shutdown")
	}
}

// TestPool_Shutdown_Immediate tests that immediate shutdown is fast and doesn't wait
func TestPool_Shutdown_Immediate(t *testing.T) {
	pool, err := NewPool(
		WithNumWorkers(2),
		WithQueueSizePerWorker(1024),
	)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}

	// Submit many slow tasks
	for i := 0; i < 100; i++ {
		pool.Submit(func() {
			time.Sleep(100 * time.Millisecond)
		})
	}

	// Give some tasks time to start
	time.Sleep(50 * time.Millisecond)

	// Immediate shutdown should be FAST (not wait for all 100 * 100ms = 10s)
	start := time.Now()
	pool.Shutdown(false)
	duration := time.Since(start)

	if !pool.IsShutdown() {
		t.Error("Pool should be shutdown")
	}

	if duration > 500*time.Millisecond {
		t.Errorf("Immediate shutdown took too long: %v (expected <500ms)", duration)
	}

	stats := pool.Stats()

	// Most tasks should be rejected (not completed)
	// Only the 2-3 tasks that started should complete
	if stats.Completed > 10 {
		t.Logf("Warning: %d tasks completed (expected ~2-5)", stats.Completed)
	}

	// Dropped should account for unexecuted tasks
	if stats.Dropped == 0 {
		t.Error("Expected some rejected tasks after immediate shutdown")
	}

	t.Logf("Shutdown time: %v, Submitted: %d, Completed: %d, Dropped: %d",
		duration, stats.Submitted, stats.Completed, stats.Dropped)
}

func TestPool_Shutdown_Multiple(t *testing.T) {
	pool, err := NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}

	// Multiple shutdowns should be safe
	pool.Shutdown(true)
	pool.Shutdown(true)
	pool.Shutdown(false)

	if !pool.IsShutdown() {
		t.Error("Pool should be shutdown")
	}
}
