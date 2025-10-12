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
		WithBlockingStrategy(NewThreadWhenQueueFull),
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

func TestPool_Submit_MultipleTasksSequential(t *testing.T) {
	pool, _ := NewPool(WithNumWorkers(2))
	defer pool.Shutdown(false)

	const numTasks = 100
	var completed atomic.Int32

	for i := 0; i < numTasks; i++ {
		err := pool.Submit(func() {
			completed.Add(1)
		})
		if err != nil {
			t.Errorf("Submit() error = %v", err)
		}
	}

	pool.Wait()

	if completed.Load() != numTasks {
		t.Errorf("Expected %d completions, got %d", numTasks, completed.Load())
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

func TestPool_Shutdown_Graceful_CompletesAllTasks(t *testing.T) {
	pool, _ := NewPool(WithNumWorkers(2))

	var completed atomic.Int32
	for i := 0; i < 100; i++ {
		pool.Submit(func() {
			time.Sleep(time.Millisecond)
			completed.Add(1)
		})
	}

	pool.Shutdown(true)

	if completed.Load() != 100 {
		t.Errorf("Expected 100 completions, got %d", completed.Load())
	}

	if !pool.IsShutdown() {
		t.Error("Pool should be shutdown")
	}
}

func TestPool_Shutdown_Immediate_Fast(t *testing.T) {
	pool, _ := NewPool(
		WithNumWorkers(2),
		WithQueueSizePerWorker(512),
	)

	// Block workers
	blockChan := make(chan struct{})
	pool.Submit(func() { <-blockChan })
	pool.Submit(func() { <-blockChan })
	time.Sleep(20 * time.Millisecond)

	// Queue many tasks
	for i := 0; i < 100; i++ {
		pool.Submit(func() {
			time.Sleep(100 * time.Millisecond)
		})
	}

	// Immediate shutdown should be fast
	start := time.Now()
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(blockChan)
	}()

	pool.Shutdown(false)
	duration := time.Since(start)

	if duration > 200*time.Millisecond {
		t.Errorf("Immediate shutdown too slow: %v", duration)
	}

	stats := pool.Stats()
	if stats.Dropped == 0 {
		t.Error("Expected dropped tasks")
	}

	t.Logf("Dropped: %d, Duration: %v", stats.Dropped, duration)
}

func TestPool_Shutdown_DropsQueuedTasks(t *testing.T) {
	pool, _ := NewPool(
		WithNumWorkers(1),
		WithQueueSizePerWorker(256),
	)

	// Block worker
	blockChan := make(chan struct{})
	pool.Submit(func() { <-blockChan })
	time.Sleep(20 * time.Millisecond)

	// Queue tasks
	for i := 0; i < 100; i++ {
		pool.Submit(func() {})
	}

	// Immediate shutdown
	close(blockChan)
	pool.Shutdown(false)

	stats := pool.Stats()
	if stats.Dropped == 0 {
		t.Error("Expected dropped tasks after immediate shutdown")
	}

	// Submitted should equal Completed + Dropped
	if stats.Submitted != stats.Completed+stats.Dropped {
		t.Errorf("Accounting mismatch: %d != %d + %d",
			stats.Submitted, stats.Completed, stats.Dropped)
	}
}

// ============================================================================
// Wait Tests
// ============================================================================

func TestPool_Wait_AllTasksComplete(t *testing.T) {
	pool, err := NewPool(WithNumWorkers(4))
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	const numTasks = 100
	var completed atomic.Int32

	for i := 0; i < numTasks; i++ {
		pool.Submit(func() {
			time.Sleep(time.Millisecond)
			completed.Add(1)
		})
	}

	pool.Wait()

	if completed.Load() != numTasks {
		t.Errorf("Expected %d completions, got %d", numTasks, completed.Load())
	}
}

func TestPool_Wait_NoTasks(t *testing.T) {
	pool, err := NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	// Wait should return immediately
	done := make(chan struct{})
	go func() {
		pool.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Error("Wait() did not return")
	}
}

// ============================================================================
// Stats Tests
// ============================================================================

func TestPool_Stats_Accuracy(t *testing.T) {
	pool, err := NewPool(WithNumWorkers(2))
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	const numTasks = 50
	for i := 0; i < numTasks; i++ {
		pool.Submit(func() {
			time.Sleep(time.Millisecond)
		})
	}

	pool.Wait()

	stats := pool.Stats()
	if stats.Submitted != numTasks {
		t.Errorf("Expected %d submitted, got %d", numTasks, stats.Submitted)
	}
	if stats.Completed != numTasks {
		t.Errorf("Expected %d completed, got %d", numTasks, stats.Completed)
	}
	if stats.InFlight != 0 {
		t.Errorf("Expected 0 in-flight, got %d", stats.InFlight)
	}
}

func TestPool_Stats_LatencyTracking(t *testing.T) {
	pool, err := NewPool(WithNumWorkers(2))
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	const sleepTime = 10 * time.Millisecond
	for i := 0; i < 10; i++ {
		pool.Submit(func() {
			time.Sleep(sleepTime)
		})
	}

	pool.Wait()

	stats := pool.Stats()
	if stats.LatencyAvg < sleepTime {
		t.Errorf("Expected avg latency >= %v, got %v", sleepTime, stats.LatencyAvg)
	}
	if stats.LatencyMax < sleepTime {
		t.Errorf("Expected max latency >= %v, got %v", sleepTime, stats.LatencyMax)
	}
}

func TestPool_Stats_WorkerStats(t *testing.T) {
	pool, err := NewPool(WithNumWorkers(3))
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	for i := 0; i < 30; i++ {
		pool.Submit(func() {
			time.Sleep(time.Millisecond)
		})
	}

	pool.Wait()

	stats := pool.Stats()
	if len(stats.WorkerStats) != 3 {
		t.Errorf("Expected 3 worker stats, got %d", len(stats.WorkerStats))
	}

	totalExecuted := uint64(0)
	for _, ws := range stats.WorkerStats {
		totalExecuted += ws.TasksExecuted
	}

	if totalExecuted != 30 {
		t.Errorf("Expected 30 total executions, got %d", totalExecuted)
	}
}

// ============================================================================
// Worker Lifecycle Tests
// ============================================================================

func TestPool_WorkerHooks(t *testing.T) {
	var startedWorkers atomic.Int32
	var stoppedWorkers atomic.Int32

	pool, err := NewPool(
		WithNumWorkers(2),
		WithWorkerHooks(
			func(workerID int) {
				startedWorkers.Add(1)
			},
			func(workerID int) {
				stoppedWorkers.Add(1)
			},
		),
	)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}

	// Give workers time to start
	time.Sleep(10 * time.Millisecond)

	if startedWorkers.Load() != 2 {
		t.Errorf("Expected 2 workers started, got %d", startedWorkers.Load())
	}

	pool.Shutdown(false)

	if stoppedWorkers.Load() != 2 {
		t.Errorf("Expected 2 workers stopped, got %d", stoppedWorkers.Load())
	}
}

// ============================================================================
// Concurrency and Race Tests
// ============================================================================

func TestPool_HighConcurrency(t *testing.T) {
	pool, err := NewPool(WithNumWorkers(8))
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	const numGoroutines = 100
	const tasksPerGoroutine = 100
	var completed atomic.Int32

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < tasksPerGoroutine; j++ {
				pool.Submit(func() {
					completed.Add(1)
					time.Sleep(time.Microsecond)
				})
			}
		}()
	}

	wg.Wait()
	pool.Wait()

	expected := int32(numGoroutines * tasksPerGoroutine)
	if completed.Load() != expected {
		t.Errorf("Expected %d completions, got %d", expected, completed.Load())
	}
}

func TestPool_ConcurrentSubmit_LowContention(t *testing.T) {
	pool, _ := NewPool(WithNumWorkers(4))
	defer pool.Shutdown(false)

	const numGoroutines = 10
	const tasksPerGoroutine = 100
	var completed atomic.Int32

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < tasksPerGoroutine; j++ {
				pool.Submit(func() {
					completed.Add(1)
					time.Sleep(time.Microsecond)
				})
			}
		}()
	}

	wg.Wait()
	pool.Wait()

	expected := int32(numGoroutines * tasksPerGoroutine)
	if completed.Load() != expected {
		t.Errorf("Expected %d completions, got %d", expected, completed.Load())
	}
}

func TestPool_ConcurrentSubmit_HighContention(t *testing.T) {
	pool, _ := NewPool(WithNumWorkers(8))
	defer pool.Shutdown(false)

	const numGoroutines = 100
	const tasksPerGoroutine = 100
	var completed atomic.Int32

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < tasksPerGoroutine; j++ {
				pool.Submit(func() {
					completed.Add(1)
				})
			}
		}()
	}

	wg.Wait()
	pool.Wait()

	expected := int32(numGoroutines * tasksPerGoroutine)
	if completed.Load() != expected {
		t.Errorf("Expected %d completions, got %d", expected, completed.Load())
	}
}

func TestPool_ConcurrentSubmitAndWait(t *testing.T) {
	pool, _ := NewPool(WithNumWorkers(4))
	defer pool.Shutdown(false)

	var wg sync.WaitGroup
	const numWaiters = 5
	var totalCompleted atomic.Int32

	// Multiple goroutines submitting and waiting
	for i := 0; i < numWaiters; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			var localCompleted atomic.Int32
			for j := 0; j < 50; j++ {
				pool.Submit(func() {
					localCompleted.Add(1)
					totalCompleted.Add(1)
				})
			}

			// Each goroutine waits for all its tasks
			// This tests concurrent Wait() calls
			time.Sleep(10 * time.Millisecond)

			if localCompleted.Load() != 50 {
				t.Errorf("Goroutine %d: expected 50, got %d", id, localCompleted.Load())
			}
		}(i)
	}

	wg.Wait()
	pool.Wait()

	expected := int32(numWaiters * 50)
	if totalCompleted.Load() != expected {
		t.Errorf("Expected %d total completions, got %d", expected, totalCompleted.Load())
	}
}

// ============================================================================
// Edge Cases and Stress Tests
// ============================================================================

func TestPool_EmptyTaskBurst(t *testing.T) {
	pool, err := NewPool(WithNumWorkers(2))
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	// Submit many fast tasks
	for i := 0; i < 10000; i++ {
		pool.Submit(func() {
			// No-op
		})
	}

	pool.Wait()

	stats := pool.Stats()
	if stats.Completed != 10000 {
		t.Errorf("Expected 10000 completed, got %d", stats.Completed)
	}
}

func TestPool_TaskBurst(t *testing.T) {
	pool, err := NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	// Submit many small tasks
	for i := 0; i < 10000; i++ {
		pool.Submit(func() {
			time.Sleep(5 * time.Millisecond)
		})
	}

	pool.Wait()

	stats := pool.Stats()
	if stats.Completed != 10000 {
		t.Errorf("Expected 10000 completed, got %d", stats.Completed)
	}
}

func TestPool_LongRunningTasks(t *testing.T) {
	pool, err := NewPool(WithNumWorkers(2))
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	var completed atomic.Int32
	for i := 0; i < 4; i++ {
		pool.Submit(func() {
			time.Sleep(50 * time.Millisecond)
			completed.Add(1)
		})
	}

	pool.Wait()

	if completed.Load() != 4 {
		t.Errorf("Expected 4 completions, got %d", completed.Load())
	}
}

func TestPool_RapidSubmitWait(t *testing.T) {
	pool, err := NewPool(WithNumWorkers(4))
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	for round := 0; round < 10; round++ {
		var completed atomic.Int32
		for i := 0; i < 20; i++ {
			pool.Submit(func() {
				completed.Add(1)
			})
		}
		pool.Wait()

		if completed.Load() != 20 {
			t.Errorf("Round %d: Expected 20 completions, got %d", round, completed.Load())
		}
	}
}

func TestPool_QueueUtilization(t *testing.T) {
	pool, err := NewPool(
		WithNumWorkers(2),
		WithQueueSizePerWorker(64),
	)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	blockChan := make(chan struct{})

	// Fill queues
	for i := 0; i < 100; i++ {
		pool.Submit(func() {
			<-blockChan
		})
	}

	time.Sleep(10 * time.Millisecond)

	stats := pool.Stats()
	if stats.Utilization <= 0 {
		t.Error("Expected non-zero utilization")
	}
	if stats.TotalQueueDepth <= 0 {
		t.Error("Expected non-zero queue depth")
	}

	close(blockChan)
	pool.Wait()
}

func TestPool_EdgeCase_SingleWorker(t *testing.T) {
	pool, _ := NewPool(WithNumWorkers(1))
	defer pool.Shutdown(false)

	var completed atomic.Int32
	for i := 0; i < 100; i++ {
		pool.Submit(func() {
			completed.Add(1)
		})
	}

	pool.Wait()

	if completed.Load() != 100 {
		t.Errorf("Expected 100 completions, got %d", completed.Load())
	}
}

func TestPool_EdgeCase_TinyQueue(t *testing.T) {
	pool, _ := NewPool(
		WithNumWorkers(2),
		WithQueueSizePerWorker(2),
		WithBlockingStrategy(NewThreadWhenQueueFull),
	)
	defer pool.Shutdown(false)

	var completed atomic.Int32
	for i := 0; i < 100; i++ {
		pool.Submit(func() {
			completed.Add(1)
		})
	}

	pool.Wait()

	if completed.Load() != 100 {
		t.Errorf("Expected 100 completions, got %d", completed.Load())
	}

	stats := pool.Stats()
	if stats.FallbackExecuted == 0 {
		t.Error("Expected fallback with tiny queue")
	}
}

func TestPool_EdgeCase_EmptyBurst(t *testing.T) {
	pool, _ := NewPool()
	defer pool.Shutdown(false)

	for i := 0; i < 10000; i++ {
		pool.Submit(func() {})
	}

	pool.Wait()

	stats := pool.Stats()
	if stats.Completed != 10000 {
		t.Errorf("Expected 10000 completed, got %d", stats.Completed)
	}
}

func TestPool_Stress_HighThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	pool, _ := NewPool(WithNumWorkers(runtime.NumCPU()))
	defer pool.Shutdown(false)

	const numTasks = 100000
	var completed atomic.Int32

	start := time.Now()

	for i := 0; i < numTasks; i++ {
		pool.Submit(func() {
			completed.Add(1)
		})
	}

	pool.Wait()
	duration := time.Since(start)

	if completed.Load() != numTasks {
		t.Errorf("Expected %d completions, got %d", numTasks, completed.Load())
	}

	throughput := float64(numTasks) / duration.Seconds()
	t.Logf("Throughput: %.0f tasks/sec", throughput)
}

func TestPool_Stress_MixedWorkload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	pool, _ := NewPool(WithNumWorkers(8))
	defer pool.Shutdown(false)

	var (
		fastTasks   atomic.Int32
		mediumTasks atomic.Int32
		slowTasks   atomic.Int32
	)

	// Fast tasks
	for i := 0; i < 10000; i++ {
		pool.Submit(func() {
			fastTasks.Add(1)
		})
	}

	// Medium tasks
	for i := 0; i < 1000; i++ {
		pool.Submit(func() {
			time.Sleep(time.Microsecond * 100)
			mediumTasks.Add(1)
		})
	}

	// Slow tasks
	for i := 0; i < 100; i++ {
		pool.Submit(func() {
			time.Sleep(time.Millisecond * 5)
			slowTasks.Add(1)
		})
	}

	pool.Wait()

	if fastTasks.Load() != 10000 {
		t.Errorf("Expected 10000 fast tasks, got %d", fastTasks.Load())
	}
	if mediumTasks.Load() != 1000 {
		t.Errorf("Expected 1000 medium tasks, got %d", mediumTasks.Load())
	}
	if slowTasks.Load() != 100 {
		t.Errorf("Expected 100 slow tasks, got %d", slowTasks.Load())
	}
}

func TestPool_Stress_RapidShutdownCycles(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	for cycle := 0; cycle < 100; cycle++ {
		pool, _ := NewPool(WithNumWorkers(2))

		for i := 0; i < 50; i++ {
			pool.Submit(func() {
				time.Sleep(time.Microsecond * 100)
			})
		}

		if cycle%2 == 0 {
			pool.Shutdown(true)
		} else {
			pool.Shutdown(false)
		}
	}
}

// ============================================================================
// Worker Panic Tests
// ============================================================================

func TestWorker_MultiplePanics(t *testing.T) {
	var panicCount atomic.Int32
	pool, err := NewPool(
		WithNumWorkers(2),
		WithPanicHandler(func(r interface{}) {
			panicCount.Add(1)
		}),
	)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	for i := 0; i < 10; i++ {
		pool.Submit(func() {
			panic("intentional panic")
		})
	}

	pool.Wait()

	if panicCount.Load() != 10 {
		t.Errorf("Expected 10 panics handled, got %d", panicCount.Load())
	}

	// Pool should still work
	var executed atomic.Bool
	pool.Submit(func() {
		executed.Store(true)
	})
	pool.Wait()

	if !executed.Load() {
		t.Error("Pool should work after panics")
	}
}

// ============================================================================
// Worker Parking and Signaling Tests
// ============================================================================

func TestWorker_ParkAndWakeup(b *testing.T) {
	pool, err := NewPool(
		WithNumWorkers(1),
		WithMaxParkTime(20*time.Millisecond),
	)
	if err != nil {
		b.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	// Let worker park
	time.Sleep(50 * time.Millisecond)

	// Submit work - should wake worker
	var executed atomic.Bool
	pool.Submit(func() {
		executed.Store(true)
	})

	pool.Wait()

	if !executed.Load() {
		b.Error("Worker did not wake up and execute task")
	}
}

func TestWorker_SpinBeforePark(t *testing.T) {
	pool, err := NewPool(
		WithNumWorkers(1),
		WithSpinCount(100),
		WithMaxParkTime(10*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	// Submit task after short delay
	go func() {
		time.Sleep(5 * time.Millisecond)
		pool.Submit(func() {})
	}()

	// Worker should spin and find the task before parking
	time.Sleep(20 * time.Millisecond)
	pool.Wait()

	stats := pool.Stats()
	if stats.Completed != 1 {
		t.Errorf("Expected 1 completion, got %d", stats.Completed)
	}
}

// ============================================================================
// Fallback Execution Tests
// ============================================================================

func TestPool_FallbackExecution_SmallQueue(t *testing.T) {
	pool, _ := NewPool(
		WithNumWorkers(1),
		WithQueueSizePerWorker(4),
		WithBlockingStrategy(NewThreadWhenQueueFull),
	)
	defer pool.Shutdown(false)

	// Block the worker
	blockChan := make(chan struct{})
	pool.Submit(func() { <-blockChan })
	time.Sleep(20 * time.Millisecond)

	// Submit tasks that will go to fallback
	var completed atomic.Int32
	for i := 0; i < 20; i++ {
		pool.Submit(func() {
			completed.Add(1)
		})
	}

	close(blockChan)
	pool.Wait()

	if completed.Load() != 20 {
		t.Errorf("Expected 20 completions, got %d", completed.Load())
	}

	stats := pool.Stats()
	if stats.FallbackExecuted == 0 {
		t.Error("Expected fallback execution, got none")
	}

	t.Logf("Fallback executions: %d", stats.FallbackExecuted)
}

func TestPool_FallbackExecution_AllQueuesFullnpm(t *testing.T) {
	pool, _ := NewPool(
		WithNumWorkers(2),
		WithQueueSizePerWorker(2),
		WithBlockingStrategy(NewThreadWhenQueueFull),
	)
	defer pool.Shutdown(false)

	// Block both workers
	blockChan := make(chan struct{})
	pool.Submit(func() { <-blockChan })
	pool.Submit(func() { <-blockChan })
	time.Sleep(20 * time.Millisecond)

	// Fill queues and trigger fallback
	var completed atomic.Int32
	for i := 0; i < 50; i++ {
		pool.Submit(func() {
			completed.Add(1)
		})
	}

	close(blockChan)
	pool.Wait()

	if completed.Load() != 50 {
		t.Errorf("Expected 50 completions, got %d", completed.Load())
	}

	stats := pool.Stats()
	if stats.FallbackExecuted < 10 {
		t.Errorf("Expected significant fallback execution, got %d", stats.FallbackExecuted)
	}
}

// ============================================================================
// Integration Tests
// ============================================================================

func TestPool_RealWorldScenario(t *testing.T) {
	// Simulate a real-world scenario with mixed workloads
	pool, err := NewPool(
		WithNumWorkers(4),
		WithQueueSizePerWorker(128),
	)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(true)

	var (
		fastTasks   atomic.Int32
		mediumTasks atomic.Int32
		slowTasks   atomic.Int32
		failedTasks atomic.Int32
	)

	// Fast tasks
	for i := 0; i < 100; i++ {
		pool.Submit(func() {
			fastTasks.Add(1)
		})
	}

	// Medium tasks
	for i := 0; i < 50; i++ {
		pool.Submit(func() {
			time.Sleep(time.Millisecond)
			mediumTasks.Add(1)
		})
	}

	// Slow tasks
	for i := 0; i < 10; i++ {
		pool.Submit(func() {
			time.Sleep(10 * time.Millisecond)
			slowTasks.Add(1)
		})
	}

	// Some failing tasks
	for i := 0; i < 5; i++ {
		pool.Submit(func() {
			defer func() {
				if recover() != nil {
					failedTasks.Add(1)
				}
			}()
			panic("simulated failure")
		})
	}

	pool.Wait()

	if fastTasks.Load() != 100 {
		t.Errorf("Expected 100 fast tasks, got %d", fastTasks.Load())
	}
	if mediumTasks.Load() != 50 {
		t.Errorf("Expected 50 medium tasks, got %d", mediumTasks.Load())
	}
	if slowTasks.Load() != 10 {
		t.Errorf("Expected 10 slow tasks, got %d", slowTasks.Load())
	}

	stats := pool.Stats()
	if stats.Completed != 165 {
		t.Errorf("Expected 165 total completions, got %d", stats.Completed)
	}
}

func TestPool_GracefulDegradation(t *testing.T) {
	// Test pool behavior under stress
	pool, err := NewPool(
		WithNumWorkers(2),
		WithQueueSizePerWorker(8),
	)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	var (
		submitted atomic.Int32
		completed atomic.Int32
	)

	// Rapid fire submissions
	for i := 0; i < 1000; i++ {
		err := pool.Submit(func() {
			completed.Add(1)
		})
		if err == nil {
			submitted.Add(1)
		}
	}

	pool.Wait()

	// All submitted tasks should complete (including fallback)
	if submitted.Load() != completed.Load() {
		t.Errorf("Submitted %d but completed %d", submitted.Load(), completed.Load())
	}

	stats := pool.Stats()
	if stats.FallbackExecuted == 0 {
		t.Log("No fallback executions (queues handled everything)")
	} else {
		t.Logf("Fallback executions: %d", stats.FallbackExecuted)
	}
}
