package pool

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

// jobItem bundles a job with its context for internal queuing.
type jobItem[T any] struct {
	ctx context.Context
	job Job[T]
}

// worker is a goroutine that executes jobs. It can self-terminate when idle.
type worker[T any] struct {
	pool  *Pool[T]
	jobCh chan jobItem[T] // Private channel for direct handoff of jobs.
}

// Pool is a high-performance, dynamic worker pool that scales automatically
// and buffers jobs when at maximum capacity.
type Pool[T any] struct {
	// Configuration
	minWorkers  int32
	maxWorkers  int32
	maxIdleTime time.Duration

	// Core components for the hybrid "Buffered Handoff" model
	workerCh       chan *worker[T] // The "breakroom" for idle workers awaiting direct handoff.
	jobs           chan jobItem[T] // The central "overflow" queue for when all workers are busy.
	results        chan Result[T]
	currentWorkers int32 // Atomic counter for the current number of workers.

	// Utilities
	resultPool sync.Pool

	// Statistics - all atomic for lock-free access
	submitted int64
	running   int64
	completed int64
	failed    int64

	// Lifecycle management
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// New creates a new dynamic worker pool with the given options.
func New[T any](opts ...Option) *Pool[T] {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	if config.MinWorkers > config.MaxWorkers {
		config.MinWorkers = config.MaxWorkers
	}

	ctx, cancel := context.WithCancel(context.Background())

	p := &Pool[T]{
		minWorkers:  int32(config.MinWorkers),
		maxWorkers:  int32(config.MaxWorkers),
		maxIdleTime: config.MaxIdleTime,
		workerCh:    make(chan *worker[T], config.MaxWorkers),
		jobs:        make(chan jobItem[T], config.ResultBuffer),
		results:     make(chan Result[T], config.ResultBuffer),
		ctx:         ctx,
		cancel:      cancel,
	}

	p.resultPool.New = func() any {
		return &Result[T]{}
	}

	// Pre-start the minimum number of workers.
	for i := 0; i < config.MinWorkers; i++ {
		p.startWorker()
	}

	return p
}

// Submit enqueues a job for execution using a background context.
func (p *Pool[T]) Submit(job Job[T]) {
	p.SubmitWithContext(context.Background(), job)
}

// SubmitWithContext enqueues a job with a specific context.
func (p *Pool[T]) SubmitWithContext(ctx context.Context, job Job[T]) {
	if p.ctx.Err() != nil {
		return
	}
	item := jobItem[T]{ctx: ctx, job: job}

	// First, try to hand off the job to an idle worker or scale up.
	if p.tryHandoffAndScale(item) {
		return
	}

	// If that fails, buffer the job.
	p.bufferJob(item)
}

// TrySubmit attempts to enqueue a job for execution without blocking.
func (p *Pool[T]) TrySubmit(job Job[T]) bool {
	if p.ctx.Err() != nil {
		return false
	}
	item := jobItem[T]{ctx: context.Background(), job: job}

	// Try a non-blocking handoff or scale-up.
	if p.tryHandoffAndScale(item) {
		return true
	}

	// If that fails, try to buffer without blocking.
	return p.tryBufferJob(item)
}

// Close gracefully shuts down the pool.
func (p *Pool[T]) Close() {
	p.closeOnce.Do(func() {
		p.cancel()
		p.wg.Wait()

		close(p.jobs)
		close(p.results)
	})
}

// Results returns the read-only channel for job outcomes.
func (p *Pool[T]) Results() <-chan Result[T] {
	return p.results
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

// Cap returns current workers of the pool.
func (p *Pool[T]) Cap() int {
	return int(atomic.LoadInt32(&p.currentWorkers))
}

// idle is the worker's state when it has no work. It waits for a job,
// times out, or receives a shutdown signal. It returns false if the worker should exit.
func (w *worker[T]) idle() bool {
	idleTimer := time.NewTimer(w.pool.maxIdleTime)
	defer idleTimer.Stop()

	// Place self in the idle worker "breakroom".
	select {
	case w.pool.workerCh <- w:
		// Now available for direct handoff.
	case <-w.pool.ctx.Done():
		return false // Pool closed.
	}

	// Wait for a job, an idle timeout, or shutdown.
	select {
	case item := <-w.jobCh:
		w.execute(item)
		return true // Continue working.
	case <-idleTimer.C:
		// Idle timeout fired. Attempt to scale down.
		for {
			current := atomic.LoadInt32(&w.pool.currentWorkers)
			if current <= w.pool.minWorkers {
				return true // Cannot scale down, continue working.
			}
			if atomic.CompareAndSwapInt32(&w.pool.currentWorkers, current, current-1) {
				return false // Successfully terminated self.
			}
		}
	case <-w.pool.ctx.Done():
		return false // Pool closed.
	}
}

// startWorker creates and starts a new worker goroutine.
func (p *Pool[T]) startWorker() {
	atomic.AddInt32(&p.currentWorkers, 1)
	p.wg.Add(1)
	w := &worker[T]{
		pool:  p,
		jobCh: make(chan jobItem[T], 1),
	}
	go w.run()
}

// startWorkerAndGiveJob is a helper to encapsulate creating a worker and giving it its first job.
func (p *Pool[T]) startWorkerAndGiveJob(item jobItem[T]) {
	p.wg.Add(1)
	w := &worker[T]{
		pool:  p,
		jobCh: make(chan jobItem[T], 1),
	}
	w.jobCh <- item
	go w.run()
}

// tryHandoffAndScale attempts to give a job to an idle worker or create a new one.
// It returns true on success.
func (p *Pool[T]) tryHandoffAndScale(item jobItem[T]) bool {
	// Fast path: try to get an idle worker.
	select {
	case w := <-p.workerCh:
		atomic.AddInt64(&p.submitted, 1)
		w.jobCh <- item
		return true
	default:
	}

	// Scaling path: if no idle workers, try to create a new one.
	for {
		current := atomic.LoadInt32(&p.currentWorkers)
		if current >= p.maxWorkers {
			return false // Cannot scale further.
		}
		if atomic.CompareAndSwapInt32(&p.currentWorkers, current, current+1) {
			atomic.AddInt64(&p.submitted, 1)
			p.startWorkerAndGiveJob(item)
			return true
		}
	}
}

// bufferJob places a job in the central queue, blocking if it's full.
func (p *Pool[T]) bufferJob(item jobItem[T]) {
	select {
	case p.jobs <- item:
		atomic.AddInt64(&p.submitted, 1)
	case <-p.ctx.Done():
	}
}

// tryBufferJob attempts to place a job in the central queue without blocking.
func (p *Pool[T]) tryBufferJob(item jobItem[T]) bool {
	select {
	case p.jobs <- item:
		atomic.AddInt64(&p.submitted, 1)
		return true
	default:
		return false
	}
}

// run is the main loop for a worker goroutine.
func (w *worker[T]) run() {
	defer w.pool.wg.Done()

	// Handle the initial job if this worker was created to handle one immediately.
	select {
	case item := <-w.jobCh:
		w.execute(item)
	default:
	}

	for {
		// Priority 1: Check the central overflow queue for work.
		select {
		case item, ok := <-w.pool.jobs:
			if !ok {
				return // Pool is closing.
			}
			w.execute(item)
			continue // Immediately re-check the central queue.
		default:
			// Central queue is empty, proceed to idle state.
		}

		// The worker is now idle and must wait for new work or a signal to shut down.
		if !w.idle() {
			return // idle() returned false, indicating a shutdown.
		}
	}
}

// execute runs a single job and handles its result and panic recovery.
func (w *worker[T]) execute(item jobItem[T]) {
	atomic.AddInt64(&w.pool.running, 1)
	defer atomic.AddInt64(&w.pool.running, -1)

	result := w.pool.resultPool.Get().(*Result[T])
	defer func() {
		if result.Error != nil {
			atomic.AddInt64(&w.pool.failed, 1)
		} else {
			atomic.AddInt64(&w.pool.completed, 1)
		}
		select {
		case w.pool.results <- *result:
		case <-w.pool.ctx.Done():
		default: // Drop if results channel is full and pool is not closing
		}
		*result = Result[T]{}
		w.pool.resultPool.Put(result)
	}()

	defer func() {
		if r := recover(); r != nil {
			result.Error = &PanicError{Value: r, Stack: string(debug.Stack())}
		}
	}()

	result.Value, result.Error = item.job(item.ctx)
}
