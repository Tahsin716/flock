package pool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Test utilities
func newTestPool[T any](opts ...Option) *Pool[T] {
	defaultOpts := []Option{
		WithMinWorkers(1),
		WithMaxWorkers(4),
		WithMaxIdleTime(100 * time.Millisecond),
		WithResultBuffer(10),
	}
	return New[T](append(defaultOpts, opts...)...)
}

func simpleJob(value int) Job[int] {
	return func(ctx context.Context) (int, error) {
		return value, nil
	}
}

func errorJob(err error) Job[int] {
	return func(ctx context.Context) (int, error) {
		return 0, err
	}
}

func slowJob(duration time.Duration, value int) Job[int] {
	return func(ctx context.Context) (int, error) {
		select {
		case <-time.After(duration):
			return value, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
}

func panicJob() Job[int] {
	return func(ctx context.Context) (int, error) {
		panic("test panic")
	}
}

// newTestJob is a helper to create a simple job for testing purposes.
func newTestJob(d time.Duration, shouldErr bool) Job[bool] {
	return func(ctx context.Context) (bool, error) {
		select {
		case <-time.After(d):
			if shouldErr {
				return false, fmt.Errorf("job failed as requested")
			}
			return true, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

func TestNewPool(t *testing.T) {
	p := New[any](WithMinWorkers(2), WithMaxWorkers(4))
	defer p.Close()

	stats := p.Stats()
	if stats.Running != 0 {
		t.Errorf("Expected 0 running jobs, got %d", stats.Running)
	}
	if p.currentWorkers != 2 {
		t.Errorf("Expected 2 initial workers, got %d", p.currentWorkers)
	}
}

func TestPoolBasicSubmission(t *testing.T) {
	pool := newTestPool[int]()
	defer pool.Close()

	// Test basic submission
	pool.Submit(simpleJob(42))

	// Check result
	select {
	case result := <-pool.Results():
		if result.Error != nil {
			t.Errorf("Unexpected error: %v", result.Error)
		}
		if result.Value != 42 {
			t.Errorf("Expected 42, got %d", result.Value)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for result")
	}
}

func TestPoolSubmissionWithContext(t *testing.T) {
	pool := newTestPool[int]()
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Submit job that takes longer than context timeout
	pool.SubmitWithContext(ctx, slowJob(200*time.Millisecond, 42))

	// Should get context cancellation error
	select {
	case result := <-pool.Results():
		if result.Error != context.DeadlineExceeded {
			t.Errorf("Expected context deadline exceeded, got: %v", result.Error)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for result")
	}
}

func TestPoolSubmit(t *testing.T) {
	p := New[bool](WithMinWorkers(1), WithMaxWorkers(2))
	defer p.Close()

	numJobs := 5
	var completedJobs int64

	for i := 0; i < numJobs; i++ {
		p.Submit(func(ctx context.Context) (bool, error) {
			atomic.AddInt64(&completedJobs, 1)
			return true, nil
		})
	}

	// Wait for jobs to complete by draining results
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numJobs; i++ {
			<-p.Results()
		}
	}()

	wg.Wait()
	p.Close()

	if atomic.LoadInt64(&completedJobs) != int64(numJobs) {
		t.Errorf("Expected %d completed jobs, got %d", numJobs, completedJobs)
	}
	if p.Stats().Submitted != int64(numJobs) {
		t.Errorf("Expected %d submitted jobs, got %d", numJobs, p.Stats().Submitted)
	}
}

func TestPoolTrySubmitSaturation(t *testing.T) {
	// Pool with 1 worker and a job buffer of size 1.
	p := New[bool](WithMinWorkers(1), WithMaxWorkers(1), WithResultBuffer(1))
	defer p.Close()

	// Use a channel to synchronize with the worker.
	workerStarted := make(chan struct{})

	// This job will occupy the single worker. It signals when it has started.
	longJob := func(ctx context.Context) (bool, error) {
		close(workerStarted) // Signal that the worker is now busy.
		time.Sleep(100 * time.Millisecond)
		return true, nil
	}

	// 1. First TrySubmit should succeed. It occupies the worker.
	if !p.TrySubmit(longJob) {
		t.Fatal("First TrySubmit should have succeeded")
	}

	// Wait until we are sure the worker has picked up the job and is busy.
	<-workerStarted

	// 2. Second TrySubmit should succeed by placing the job in the buffer.
	if !p.TrySubmit(newTestJob(10*time.Millisecond, false)) {
		t.Fatal("Second TrySubmit should have succeeded by buffering")
	}

	// 3. The pool is now fully saturated (worker busy, buffer full).
	//    This third TrySubmit MUST fail.
	if p.TrySubmit(newTestJob(10*time.Millisecond, false)) {
		t.Fatal("Third TrySubmit should have failed on a saturated pool")
	}

	// Allow time for jobs to complete to avoid race on close.
	time.Sleep(150 * time.Millisecond)
}

func TestPoolScaling(t *testing.T) {
	p := New[bool](WithMinWorkers(1), WithMaxWorkers(4), WithMaxIdleTime(50*time.Millisecond))
	defer p.Close()

	// Scale up to max workers
	for i := 0; i < 4; i++ {
		p.Submit(newTestJob(100*time.Millisecond, false))
	}

	// Give time for workers to spin up, check with retry
	var workers int32
	for i := 0; i < 10; i++ {
		workers = atomic.LoadInt32(&p.currentWorkers)
		if workers == 4 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if workers != 4 {
		t.Errorf("Expected pool to scale up to 4 workers, got %d", workers)
	}

	// Wait for jobs to finish and idle time to pass for scale down
	time.Sleep(200 * time.Millisecond)
	if atomic.LoadInt32(&p.currentWorkers) != 1 {
		t.Errorf("Expected pool to scale down to 1 worker, got %d", p.currentWorkers)
	}
}

func TestPoolClose(t *testing.T) {
	p := New[bool](WithMinWorkers(2), WithMaxWorkers(4))
	var completedJobs int64

	for i := 0; i < 10; i++ {
		p.Submit(func(ctx context.Context) (bool, error) {
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt64(&completedJobs, 1)
			return true, nil
		})
	}

	time.Sleep(10 * time.Millisecond) // Allow some jobs to start
	p.Close()                         // Should block until all 10 jobs are done

	// Drain the results channel before checking its state.
	for i := 0; i < int(atomic.LoadInt64(&completedJobs)); i++ {
		<-p.Results()
	}

	if atomic.LoadInt64(&completedJobs) != 10 {
		t.Errorf("Not all jobs were completed before Close returned, completed: %d", completedJobs)
	}

	// Submitting to a closed pool should be a no-op
	p.Submit(newTestJob(10*time.Millisecond, false))
	if p.Stats().Submitted != 10 {
		t.Errorf("Job should not be submitted to a closed pool")
	}

	// Results channel should now be closed and empty
	select {
	case _, ok := <-p.Results():
		if ok {
			t.Error("Results channel should be closed and empty")
		}
	default:
		// This case is expected for a closed and empty channel, but select is non-deterministic.
		// The check above is the primary one.
	}
}

func TestPoolConcurrency(t *testing.T) {
	numGoroutines := 100
	tasksPerGoroutine := 20
	totalTasks := numGoroutines * tasksPerGoroutine

	p := New[bool](
		WithMinWorkers(8),
		WithMaxWorkers(64),
		WithResultBuffer(totalTasks),
	)

	var submitWg sync.WaitGroup
	submitWg.Add(numGoroutines)

	// Phase 1: Submit all tasks from multiple goroutines
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer submitWg.Done()
			for j := 0; j < tasksPerGoroutine; j++ {
				p.Submit(func(ctx context.Context) (bool, error) {
					time.Sleep(time.Millisecond)
					return true, nil
				})
			}
		}()
	}

	var completedJobs int64

	// Phase 2: Concurrently drain all results
	var drainWg sync.WaitGroup
	drainWg.Add(1)
	go func() {
		defer drainWg.Done()
		for i := 0; i < totalTasks; i++ {
			<-p.Results()
			atomic.AddInt64(&completedJobs, 1)
		}
	}()

	submitWg.Wait()

	drainWg.Wait()
	p.Close()

	stats := p.Stats()
	if stats.Submitted != int64(totalTasks) {
		t.Errorf("Expected submitted to be %d, got %d", totalTasks, stats.Submitted)
	}
	if atomic.LoadInt64(&completedJobs) != int64(totalTasks) {
		t.Errorf("Expected completed to be %d, got %d", totalTasks, atomic.LoadInt64(&completedJobs))
	}
}

func TestPoolTrySubmit(t *testing.T) {
	// Create pool with small buffer to test blocking behavior
	pool := newTestPool[int](WithResultBuffer(1))
	defer pool.Close()

	// Fill the queue
	success := pool.TrySubmit(slowJob(100*time.Millisecond, 1))
	if !success {
		t.Error("First TrySubmit should succeed")
	}

	// This might fail if buffer is full
	_ = pool.TrySubmit(slowJob(100*time.Millisecond, 2))

	// At least one should complete
	timeout := time.After(200 * time.Millisecond)
	resultCount := 0
	for resultCount < 2 {
		select {
		case <-pool.Results():
			resultCount++
		case <-timeout:
			if resultCount == 0 {
				t.Error("Should have received at least one result")
			}
			return
		}
	}
}

func TestPoolErrorHandling(t *testing.T) {
	pool := newTestPool[int]()
	defer pool.Close()

	testErr := errors.New("test error")
	pool.Submit(errorJob(testErr))

	select {
	case result := <-pool.Results():
		if result.Error != testErr {
			t.Errorf("Expected test error, got: %v", result.Error)
		}
		if result.Value != 0 {
			t.Errorf("Expected 0 for error case, got %d", result.Value)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for result")
	}
}

func TestPoolClosedSubmission(t *testing.T) {
	pool := newTestPool[int]()
	pool.Close()

	pool.Submit(simpleJob(42))

	result := <-pool.Results()
	if result.Error != nil {
		t.Error("Submit should fail on closed pool")
	}

	success := pool.TrySubmit(simpleJob(42))
	if success {
		t.Error("TrySubmit should fail on closed pool")
	}
}

func TestPoolStats(t *testing.T) {
	pool := newTestPool[int]()
	defer pool.Close()

	// Initial stats
	stats := pool.Stats()
	if stats.Submitted != 0 || stats.Running != 0 || stats.Completed != 0 || stats.Failed != 0 {
		t.Errorf("Expected zero stats initially, got: %+v", stats)
	}

	// Submit jobs
	for i := 0; i < 5; i++ {
		pool.Submit(simpleJob(i))
	}
	pool.Submit(errorJob(errors.New("test")))

	// Wait for completion
	for i := 0; i < 6; i++ {
		<-pool.Results()
	}

	stats = pool.Stats()
	if stats.Submitted != 6 {
		t.Errorf("Expected 6 submitted, got %d", stats.Submitted)
	}
	if stats.Completed != 5 {
		t.Errorf("Expected 5 completed, got %d", stats.Completed)
	}
	if stats.Failed != 1 {
		t.Errorf("Expected 1 failed, got %d", stats.Failed)
	}
}

// Worker scaling tests

func TestPoolWorkerScaling(t *testing.T) {
	pool := newTestPool[int](
		WithMinWorkers(1),
		WithMaxWorkers(4),
		WithMaxIdleTime(50*time.Millisecond),
	)
	defer pool.Close()

	// Submit multiple slow jobs to trigger scaling
	for i := 0; i < 8; i++ {
		pool.Submit(slowJob(100*time.Millisecond, i))
	}

	// Wait a bit for scaling
	time.Sleep(20 * time.Millisecond)

	// Should have scaled up due to queue pressure
	stats := pool.Stats()
	if stats.Running == 0 {
		t.Error("Expected some workers to be running")
	}

	// Drain results
	for i := 0; i < 8; i++ {
		<-pool.Results()
	}

	// Wait for idle timeout to trigger scaling down
	time.Sleep(100 * time.Millisecond)
}

// Concurrency and race condition tests

func TestPoolConcurrentSubmission(t *testing.T) {
	pool := newTestPool[int]()
	defer pool.Close()

	const numGoroutines = 100
	const jobsPerGoroutine = 10

	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < jobsPerGoroutine; j++ {
				pool.Submit(simpleJob(id*jobsPerGoroutine + j))
			}
		}(i)
	}

	wg.Wait()

	// Collect results
	expectedResults := numGoroutines * jobsPerGoroutine
	for i := 0; i < expectedResults; i++ {
		select {
		case result := <-pool.Results():
			if result.Error != nil {
				t.Errorf("Unexpected error in result %d: %v", i, result.Error)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Timeout waiting for result %d", i)
		}
	}

	stats := pool.Stats()
	if stats.Submitted != int64(expectedResults) {
		t.Errorf("Expected %d submitted, got %d", expectedResults, stats.Submitted)
	}
}
