package pool

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Helper function to create a simple job for testing.
func newTestJob(duration time.Duration, shouldError bool) Job[bool] {
	return func(ctx context.Context) (bool, error) {
		select {
		case <-time.After(duration):
			if shouldError {
				return false, context.DeadlineExceeded
			}
			return true, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

func TestNewPool(t *testing.T) {
	t.Run("DefaultConfig", func(t *testing.T) {
		p := New[bool]()
		defer p.Close()
		config := DefaultConfig()
		if p.minWorkers != int32(config.MinWorkers) || p.maxWorkers != int32(config.MaxWorkers) {
			t.Errorf("New() with default config failed, got min:%d max:%d", p.minWorkers, p.maxWorkers)
		}
		if p.Stats().Running != 0 || atomic.LoadInt32(&p.currentWorkers) != p.minWorkers {
			t.Errorf("Initial workers count is incorrect")
		}
	})

	t.Run("WithCustomOptions", func(t *testing.T) {
		p := New[bool](WithMinWorkers(5), WithMaxWorkers(10))
		defer p.Close()
		if p.minWorkers != 5 || p.maxWorkers != 10 {
			t.Errorf("New() with custom options failed, got min:%d max:%d", p.minWorkers, p.maxWorkers)
		}
	})

	t.Run("ValidationLogic", func(t *testing.T) {
		p := New[bool](WithMinWorkers(10), WithMaxWorkers(5)) // min > max
		defer p.Close()
		if p.minWorkers != 5 || p.maxWorkers != 5 {
			t.Errorf("Validation for min > max failed, got min:%d max:%d", p.minWorkers, p.maxWorkers)
		}
	})
}

func TestPoolSubmitAndResults(t *testing.T) {
	p := New[bool](WithMinWorkers(1), WithMaxWorkers(2))
	defer p.Close()

	p.Submit(newTestJob(10*time.Millisecond, false))
	p.Submit(newTestJob(10*time.Millisecond, true))

	results := 0
	for i := 0; i < 2; i++ {
		res := <-p.Results()
		results++
		if res.Error != nil && res.Error != context.DeadlineExceeded {
			t.Errorf("Expected a specific error, but got %v", res.Error)
		}
	}

	if results != 2 {
		t.Errorf("Expected 2 results, got %d", results)
	}

	stats := p.Stats()
	if stats.Submitted != 2 || stats.Completed != 1 || stats.Failed != 1 {
		t.Errorf("Stats are incorrect after jobs, got %+v", stats)
	}
}

func TestPoolScaling(t *testing.T) {
	p := New[bool](WithMinWorkers(1), WithMaxWorkers(3), WithMaxIdleTime(50*time.Millisecond))
	defer p.Close()

	if atomic.LoadInt32(&p.currentWorkers) != 1 {
		t.Fatalf("Initial worker count should be 1, got %d", atomic.LoadInt32(&p.currentWorkers))
	}

	// Force scaling up to max
	for i := 0; i < 3; i++ {
		p.Submit(newTestJob(100*time.Millisecond, false))
	}

	time.Sleep(20 * time.Millisecond) // Give time for scaling to occur
	if atomic.LoadInt32(&p.currentWorkers) != 3 {
		t.Fatalf("Pool did not scale up to 3 workers, got %d", atomic.LoadInt32(&p.currentWorkers))
	}

	// Wait for jobs to finish and idle time to pass for scale down
	time.Sleep(200 * time.Millisecond)

	if atomic.LoadInt32(&p.currentWorkers) != 1 {
		t.Errorf("Pool did not scale down to 1 worker, got %d", atomic.LoadInt32(&p.currentWorkers))
	}
}

func TestPoolTrySubmit(t *testing.T) {
	p := New[bool](WithMinWorkers(1), WithMaxWorkers(1)) // Fixed size for testing saturation
	defer p.Close()

	// First submit should succeed and occupy the only worker
	if !p.TrySubmit(newTestJob(100*time.Millisecond, false)) {
		t.Fatal("First TrySubmit should have succeeded")
	}

	// Second submit should fail as the pool is saturated
	if p.TrySubmit(newTestJob(10*time.Millisecond, false)) {
		t.Fatal("Second TrySubmit should have failed on a saturated pool")
	}

	time.Sleep(150 * time.Millisecond) // Wait for the first job to complete

	// Third submit should now succeed
	if !p.TrySubmit(newTestJob(10*time.Millisecond, false)) {
		t.Fatal("Third TrySubmit should have succeeded after worker became free")
	}
}

func TestPoolPanicHandling(t *testing.T) {
	p := New[bool]()
	defer p.Close()

	panicJob := func(ctx context.Context) (bool, error) {
		panic("test panic")
	}

	p.Submit(panicJob)

	res := <-p.Results()
	if res.Error == nil {
		t.Fatal("Expected an error from a panicking job, but got nil")
	}

	panicErr, ok := res.Error.(*PanicError)
	if !ok {
		t.Fatalf("Expected error to be of type PanicError, but got %T", res.Error)
	}
	if panicErr.Value != "test panic" {
		t.Errorf("Panic value was incorrect, got %v", panicErr.Value)
	}

	if p.Stats().Failed != 1 {
		t.Errorf("Failed stats should be 1 after a panic, got %d", p.Stats().Failed)
	}
}

func TestPoolClose(t *testing.T) {
	p := New[bool](WithMinWorkers(2), WithMaxWorkers(4), WithResultBuffer(10))
	var completedJobs int64
	numJobs := 10

	for i := 0; i < numJobs; i++ {
		p.Submit(func(ctx context.Context) (bool, error) {
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt64(&completedJobs, 1)
			return true, nil
		})
	}

	time.Sleep(10 * time.Millisecond) // Allow some jobs to start
	p.Close()                         // Should block until all 10 jobs are done

	if atomic.LoadInt64(&completedJobs) != int64(numJobs) {
		t.Errorf("Not all jobs were completed before Close returned, completed: %d", completedJobs)
	}

	// Submitting to a closed pool should be a no-op
	p.Submit(newTestJob(10*time.Millisecond, false))
	if p.Stats().Submitted != int64(numJobs) {
		t.Errorf("Job should not be submitted to a closed pool")
	}

	// Drain the results channel. Since Close() has returned, we know all jobs
	// are done and the results channel is now closed.
	for i := 0; i < numJobs; i++ {
		<-p.Results()
	}

	// Now that the channel is drained, a read should confirm it's closed.
	_, ok := <-p.Results()
	if ok {
		t.Error("Results channel should be closed and empty, but a value was received.")
	}
}

func TestPoolConcurrency(t *testing.T) {
	p := New[bool](WithMinWorkers(4), WithMaxWorkers(64), WithResultBuffer(2000))
	defer p.Close()

	numGoroutines := 100
	jobsPerGoroutine := 20
	totalJobs := numGoroutines * jobsPerGoroutine
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < jobsPerGoroutine; j++ {
				p.Submit(newTestJob(time.Millisecond, false))
			}
		}()
	}

	wg.Wait()

	completed := 0
	for i := 0; i < totalJobs; i++ {
		<-p.Results()
		completed++
	}

	if completed != totalJobs {
		t.Errorf("Expected %d completed jobs, got %d", totalJobs, completed)
	}
	stats := p.Stats()
	if stats.Submitted != int64(totalJobs) || stats.Completed != int64(totalJobs) || stats.Failed != 0 {
		t.Errorf("Stats are incorrect after concurrent execution, got %+v", stats)
	}
}
