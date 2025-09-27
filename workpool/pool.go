package workpool

import (
	"context"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// Job represents a unit of work that returns a value of type T and an error.
type Job[T any] func(ctx context.Context) (T, error)

// Result holds the outcome of a job's execution.
type Result[T any] struct {
	Value T
	Error error
}

// jobWithContext is an internal struct to bundle a job with its context.
type jobWithContext[T any] struct {
	ctx context.Context
	job Job[T]
}

// worker is a goroutine that executes jobs. It can self-terminate when idle.
type worker[T any] struct {
	pool  *Pool[T]
	jobCh chan jobWithContext[T]
}

// Pool is a high-performance, dynamic worker pool that scales automatically.
type Pool[T any] struct {
	// Worker management
	workerCh       chan *worker[T] // Channel of available workers.
	minWorkers     int32
	maxWorkers     int32
	currentWorkers int32 // Atomic counter for the current number of workers.

	// Result handling
	results    chan Result[T]
	resultPool sync.Pool // Recycles Result objects to reduce allocations.

	// Statistics - all atomic for lock-free access
	submitted int64
	running   int64
	completed int64
	failed    int64

	// Lifecycle management
	closeOnce sync.Once
	stopCh    chan struct{}
	wg        sync.WaitGroup

	// Configuration
	maxIdleTime time.Duration
}

// New creates a new dynamic worker pool with the given options.
func New[T any](opts ...Option) *Pool[T] {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	p := &Pool[T]{
		workerCh:    make(chan *worker[T], config.MaxWorkers),
		results:     make(chan Result[T], config.ResultBuffer),
		stopCh:      make(chan struct{}),
		minWorkers:  int32(config.MinWorkers),
		maxWorkers:  int32(config.MaxWorkers),
		maxIdleTime: config.MaxIdleTime,
	}

	// Initialize the pool for recycling Result structs.
	p.resultPool.New = func() any {
		return &Result[T]{}
	}

	// Pre-start the minimum number of workers.
	for i := 0; i < config.MinWorkers; i++ {
		p.wg.Add(1)
		atomic.AddInt32(&p.currentWorkers, 1)
		w := &worker[T]{
			pool:  p,
			jobCh: make(chan jobWithContext[T], 1),
		}
		go w.run()
		p.workerCh <- w // Add the new worker to the available channel.
	}

	return p
}

// Submit enqueues a job for execution, blocking if the pool is at max capacity.
// It uses a background context.
func (p *Pool[T]) Submit(job Job[T]) {
	p.SubmitWithContext(context.Background(), job)
}

// SubmitWithContext enqueues a job with a specific context.
// It will block if the pool is at maximum capacity until a worker is free.
// It may spin up a new worker if the pool is below maximum capacity.
func (p *Pool[T]) SubmitWithContext(ctx context.Context, job Job[T]) {
	if p.isClosed() {
		// Silently drop if closed to avoid panic
		return
	}

	jwc := jobWithContext[T]{ctx: ctx, job: job}

	// Fast path: try to get an idle worker.
	select {
	case w := <-p.workerCh:
		atomic.AddInt64(&p.submitted, 1)
		w.jobCh <- jwc
		return
	default:
		// No idle worker, try to scale or wait.
	}

	// Scaling path: if we are not at max capacity, create a new worker.
	if current := atomic.LoadInt32(&p.currentWorkers); current < p.maxWorkers {
		// Use CompareAndSwap to ensure we don't exceed maxWorkers in a race.
		if atomic.CompareAndSwapInt32(&p.currentWorkers, current, current+1) {
			atomic.AddInt64(&p.submitted, 1)
			p.wg.Add(1)
			w := &worker[T]{
				pool:  p,
				jobCh: make(chan jobWithContext[T], 1),
			}
			go w.run()
			w.jobCh <- jwc // Give the job to the new worker.
			return
		}
	}

	// Slow path: pool is at max capacity, wait for an available worker.
	select {
	case w := <-p.workerCh:
		atomic.AddInt64(&p.submitted, 1)
		w.jobCh <- jwc
	case <-p.stopCh:
		// Pool was closed while waiting. Job is not submitted.
	}
}

// TrySubmit attempts to enqueue a job for execution without blocking.
// It returns true if the job was successfully submitted, and false otherwise.
// A job will not be submitted if the pool is at maximum capacity and all workers are busy.
func (p *Pool[T]) TrySubmit(job Job[T]) bool {
	if p.isClosed() {
		return false
	}

	jwc := jobWithContext[T]{ctx: context.Background(), job: job}

	// Fast path: try to get an idle worker without blocking.
	select {
	case w := <-p.workerCh:
		atomic.AddInt64(&p.submitted, 1)
		w.jobCh <- jwc
		return true
	default:
		// No idle worker, fall through to scaling path.
	}

	// Scaling path: try to create a new worker without blocking.
	if current := atomic.LoadInt32(&p.currentWorkers); current < p.maxWorkers {
		if atomic.CompareAndSwapInt32(&p.currentWorkers, current, current+1) {
			atomic.AddInt64(&p.submitted, 1)
			p.wg.Add(1)
			w := &worker[T]{
				pool:  p,
				jobCh: make(chan jobWithContext[T], 1),
			}
			go w.run()
			w.jobCh <- jwc
			return true
		}
	}

	// If both paths fail, the pool is saturated. Return false.
	return false
}

// Results returns the read-only channel for job outcomes.
// If the result channel buffer is full, new results will be dropped.
func (p *Pool[T]) Results() <-chan Result[T] {
	return p.results
}

// Close gracefully shuts down the pool, waiting for all active jobs to finish.
func (p *Pool[T]) Close() {
	p.closeOnce.Do(func() {
		close(p.stopCh)
		p.wg.Wait()
		close(p.results)
	})
}

// Stats returns current statistics about the Pool.
func (p *Pool[T]) Stats() Stats {
	return Stats{
		Submitted: atomic.LoadInt64(&p.submitted),
		Running:   atomic.LoadInt64(&p.running),
		Completed: atomic.LoadInt64(&p.completed),
		Failed:    atomic.LoadInt64(&p.failed),
	}
}

func (p *Pool[T]) isClosed() bool {
	select {
	case <-p.stopCh:
		return true
	default:
		return false
	}
}

// worker's main execution loop.
func (w *worker[T]) run() {
	defer w.pool.wg.Done()
	idleTimer := time.NewTimer(w.pool.maxIdleTime)
	defer idleTimer.Stop()

	for {
		select {
		case jwc := <-w.jobCh:
			// Stop the idle timer, we have work to do.
			if !idleTimer.Stop() {
				// Drain timer if it fired while we were waiting for a job.
				// This is a safe way to handle a timer that might have already expired.
				select {
				case <-idleTimer.C:
				default:
				}
			}

			w.execute(jwc)

			// Return worker to the pool for reuse.
			select {
			case w.pool.workerCh <- w:
			case <-w.pool.stopCh:
				// Pool is closing, exit.
				return
			}
			idleTimer.Reset(w.pool.maxIdleTime)

		case <-idleTimer.C:
			// Worker has been idle for too long. Check if we can scale down.
			if current := atomic.LoadInt32(&w.pool.currentWorkers); current > w.pool.minWorkers {
				if atomic.CompareAndSwapInt32(&w.pool.currentWorkers, current, current-1) {
					// Successfully decremented count, self-terminate.
					return
				}
			}
			// We are at min workers (or failed the CAS), so stay alive.
			idleTimer.Reset(w.pool.maxIdleTime)

		case <-w.pool.stopCh:
			// Pool is closing, exit.
			return
		}
	}
}

// execute runs a single job and handles its result and panic recovery.
func (w *worker[T]) execute(jwc jobWithContext[T]) {
	atomic.AddInt64(&w.pool.running, 1)
	defer atomic.AddInt64(&w.pool.running, -1)

	result := w.pool.resultPool.Get().(*Result[T])

	defer func() {
		if result.Error != nil {
			atomic.AddInt64(&w.pool.failed, 1)
		} else {
			atomic.AddInt64(&w.pool.completed, 1)
		}

		// Send the result to the results channel.
		select {
		case w.pool.results <- *result:
		case <-w.pool.stopCh: // Don't block if pool is closing
		default:
			// Drop result if the results channel is full.
		}

		// Reset and return the Result object to the sync.Pool.
		var zero T
		result.Value = zero
		result.Error = nil
		w.pool.resultPool.Put(result)
	}()

	// Panic recovery.
	defer func() {
		if r := recover(); r != nil {
			result.Error = &PanicError{
				Value: r,
				Stack: string(debug.Stack()),
			}
		}
	}()

	// Execute the job.
	result.Value, result.Error = jwc.job(jwc.ctx)
}
