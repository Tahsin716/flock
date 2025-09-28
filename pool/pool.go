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

// Submit enqueues a job for execution using a background context.
func (p *Pool[T]) Submit(job Job[T]) {
	p.SubmitWithContext(context.Background(), job)
}

// SubmitWithContext enqueues a job with a specific context, using the Buffered Handoff strategy.
func (p *Pool[T]) SubmitWithContext(ctx context.Context, job Job[T]) {
	item := jobItem[T]{ctx: ctx, job: job}

	select {
	case <-p.ctx.Done(): // Check if pool is closed first.
		return
	default:
	}

	// FAST PATH (Handoff): Try to give the job to an idle worker.
	select {
	case w := <-p.workerCh:
		atomic.AddInt64(&p.submitted, 1)
		w.jobCh <- item
		return
	default:
	}

	// SCALING PATH: If no idle workers, try to create a new one.
	for {
		current := atomic.LoadInt32(&p.currentWorkers)
		if current >= p.maxWorkers {
			break // At max capacity.
		}
		if atomic.CompareAndSwapInt32(&p.currentWorkers, current, current+1) {
			atomic.AddInt64(&p.submitted, 1)
			p.startWorkerAndGiveJob(item)
			return
		}
	}

	// BUFFERING PATH: Pool is saturated, place job in the central queue.
	select {
	case p.jobs <- item:
		atomic.AddInt64(&p.submitted, 1)
	case <-p.ctx.Done(): // Pool was closed while we were trying to buffer.
	}
}

// TrySubmit attempts to enqueue a job for execution without blocking.
func (p *Pool[T]) TrySubmit(job Job[T]) bool {
	item := jobItem[T]{ctx: context.Background(), job: job}

	select {
	case <-p.ctx.Done():
		return false
	default:
	}

	// FAST PATH (Handoff): Try to give the job to an idle worker.
	select {
	case w := <-p.workerCh:
		atomic.AddInt64(&p.submitted, 1)
		w.jobCh <- item
		return true
	default:
	}

	// SCALING PATH: Try to create a new worker without blocking.
	current := atomic.LoadInt32(&p.currentWorkers)
	if current < p.maxWorkers {
		if atomic.CompareAndSwapInt32(&p.currentWorkers, current, current+1) {
			atomic.AddInt64(&p.submitted, 1)
			p.startWorkerAndGiveJob(item)
			return true
		}
	}

	// BUFFERING PATH: Try to place job in the central queue without blocking.
	select {
	case p.jobs <- item:
		atomic.AddInt64(&p.submitted, 1)
		return true
	default:
	}

	return false
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

// Close gracefully shuts down the pool.
func (p *Pool[T]) Close() {
	p.closeOnce.Do(func() {
		p.cancel()
		p.wg.Wait()
		close(p.jobs)
		// Drain remaining results after all workers are done
		go func() {
			for range p.results {
			}
		}()
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

// run is the main loop for a worker goroutine.
func (w *worker[T]) run() {
	defer w.pool.wg.Done()
	idleTimer := time.NewTimer(w.pool.maxIdleTime)
	defer idleTimer.Stop()

	// Handle the initial job if this worker was created via startWorkerAndGiveJob
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
				// The central queue is closed, which only happens on shutdown.
				return
			}
			w.execute(item)
			continue // Immediately check for more buffered work.
		default:
			// Central queue is empty, proceed to idle state.
		}

		// The worker is now idle. It places itself in the breakroom.
		select {
		case w.pool.workerCh <- w:
			// Worker is now available for a fast-path handoff.
		case <-w.pool.ctx.Done():
			return // Pool closed while trying to become idle.
		}

		// Wait for either a direct-handoff job or for the idle timer to fire.
		select {
		case item := <-w.jobCh:
			// Got a direct-handoff job.
			if !idleTimer.Stop() {
				// Safely drain the timer
				select {
				case <-idleTimer.C:
				default:
				}
			}
			w.execute(item)
			idleTimer.Reset(w.pool.maxIdleTime)

		case <-idleTimer.C:
			// Idle timeout fired. Attempt to scale down.
			for {
				current := atomic.LoadInt32(&w.pool.currentWorkers)
				if current <= w.pool.minWorkers {
					break // Cannot scale down further.
				}
				if atomic.CompareAndSwapInt32(&w.pool.currentWorkers, current, current-1) {
					return // Successfully terminated self.
				}
			}
			idleTimer.Reset(w.pool.maxIdleTime)

		case <-w.pool.ctx.Done():
			return // Pool is closing.
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
