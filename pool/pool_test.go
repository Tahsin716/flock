package pool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

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
