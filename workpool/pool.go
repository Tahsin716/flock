package workpool

import (
	"context"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// Job represents a unit of work
type Job[T any] func(context.Context) (T, error)

// Result holds the outcome of a job
type Result[T any] struct {
	Value T
	Error error
}

// worker represents a reusable goroutine
type worker[T any] struct {
	jobCh chan Job[T]
	pool  *Pool[T]

	// Last activity for cleanup
	lastUsed int64 // atomic timestamp
}

// Pool is a high-performance worker pool
type Pool[T any] struct {
	// Core worker management - lock-free where possible
	workers  []*worker[T]    // Pre-allocated worker slice
	workerCh chan *worker[T] // Available workers (acts as semaphore)
	capacity int32           // Pool capacity

	// Result handling
	results    chan Result[T]
	resultPool sync.Pool // Recycle result objects

	// Statistics - all atomic for lock-free access
	running   int64 // Currently executing jobs
	submitted int64 // Total submitted jobs
	completed int64 // Successfully completed jobs
	failed    int64 // Failed jobs

	// Lifecycle management
	closed    int64 // atomic flag
	closeOnce sync.Once
	stopCh    chan struct{}
	wg        sync.WaitGroup

	// Configuration
	maxIdleTime     time.Duration
	cleanupInterval time.Duration
}

// Stats holds pool statistics
type Stats struct {
	Capacity  int64 // Maximum workers
	Available int64 // Available workers
	Running   int64 // Currently running jobs
	Submitted int64 // Total submitted jobs
	Completed int64 // Successfully completed jobs
	Failed    int64 // Failed jobs
}

// New creates a new high-performance worker pool
func New[T any](opts ...Option) *Pool[T] {
	config := DefaultConfig()

	for _, opt := range opts {
		opt(&config)
	}

	p := &Pool[T]{
		workers:         make([]*worker[T], 0, config.MaxWorkers),
		workerCh:        make(chan *worker[T], config.MaxWorkers),
		capacity:        int32(config.MaxWorkers),
		results:         make(chan Result[T], config.ResultBuffer),
		stopCh:          make(chan struct{}),
		maxIdleTime:     config.MaxIdleTime,
		cleanupInterval: config.CleanupInterval,
	}

	// Initialize result pool
	p.resultPool.New = func() interface{} {
		return &Result[T]{}
	}

	// Pre-allocate all workers to avoid allocation overhead
	for i := 0; i < config.MaxWorkers; i++ {
		w := &worker[T]{
			jobCh: make(chan Job[T], 1),
			pool:  p,
		}
		w.lastUsed = time.Now().UnixNano()
		p.workers = append(p.workers, w)
		p.workerCh <- w // All workers start available

		w.pool.wg.Add(1)
		go w.run()
	}

	// Start cleanup goroutine for idle workers
	if config.CleanupInterval > 0 {
		p.wg.Add(1)
		go p.cleanupLoop()
	}

	return p
}

// Submit submits a job for execution (blocking)
func (p *Pool[T]) Submit(job Job[T]) error {
	return p.SubmitWithContext(context.Background(), job)
}

// SubmitWithContext submits a job with context support (blocking)
func (p *Pool[T]) SubmitWithContext(ctx context.Context, job Job[T]) error {
	if atomic.LoadInt64(&p.closed) == 1 {
		return ErrPoolClosed
	}

	atomic.AddInt64(&p.submitted, 1)

	// Fast path: try to get an available worker
	select {
	case w := <-p.workerCh:
		// Got a worker, dispatch job immediately
		select {
		case w.jobCh <- job:
			return nil
		case <-ctx.Done():
			// Return worker to pool if context cancelled
			p.workerCh <- w
			return ctx.Err()
		case <-p.stopCh:
			p.workerCh <- w
			return ErrPoolClosed
		}

	case <-ctx.Done():
		return ctx.Err()
	case <-p.stopCh:
		return ErrPoolClosed
	}
}

// TrySubmit attempts to submit a job without blocking
func (p *Pool[T]) TrySubmit(job Job[T]) error {
	if atomic.LoadInt64(&p.closed) == 1 {
		return ErrPoolClosed
	}

	select {
	case w := <-p.workerCh:
		atomic.AddInt64(&p.submitted, 1)
		select {
		case w.jobCh <- job:
			return nil
		default:
			// This shouldn't happen since we have the worker
			p.workerCh <- w
			return ErrWorkerBusy
		}
	default:
		return ErrPoolFull
	}
}

// Close gracefully shuts down the pool
func (p *Pool[T]) Close() {
	p.closeOnce.Do(func() {
		atomic.StoreInt64(&p.closed, 1)
		close(p.stopCh)
		p.wg.Wait()
		close(p.results)
	})
}

// Results returns the results channel
func (p *Pool[T]) Results() <-chan Result[T] {
	return p.results
}

// Capacity returns the maximum number of workers
func (p *Pool[T]) Capacity() int {
	return int(p.capacity)
}

// Available returns the number of available workers
func (p *Pool[T]) Available() int {
	return len(p.workerCh)
}

// Running returns the number of currently running jobs
func (p *Pool[T]) Running() int64 {
	return atomic.LoadInt64(&p.running)
}

// worker run loop
func (w *worker[T]) run() {
	defer w.pool.wg.Done()

	for {
		select {
		case job := <-w.jobCh:
			w.execute(job)
			// Immediately return to pool for reuse
			select {
			case w.pool.workerCh <- w:
				// Successfully returned to pool
			case <-w.pool.stopCh:
				return
			}

		case <-w.pool.stopCh:
			return
		}
	}
}

// execute runs a job with panic recovery
func (w *worker[T]) execute(job Job[T]) {
	// Update last used time
	atomic.StoreInt64(&w.lastUsed, time.Now().UnixNano())

	// Track running job
	atomic.AddInt64(&w.pool.running, 1)
	defer atomic.AddInt64(&w.pool.running, -1)

	// Get result object from pool
	result := w.pool.resultPool.Get().(*Result[T])
	defer func() {
		// Send result and return object to pool
		select {
		case w.pool.results <- *result:
		case <-w.pool.stopCh:
		default:
			// Results channel full, drop result
		}

		// Reset and return to pool
		var zero T
		result.Value = zero
		result.Error = nil
		w.pool.resultPool.Put(result)
	}()

	// Execute with panic recovery
	func() {
		defer func() {
			if r := recover(); r != nil {
				atomic.AddInt64(&w.pool.failed, 1)
				result.Error = &PanicError{
					Value: r,
					Stack: string(debug.Stack()),
				}
				var zero T
				result.Value = zero
			}
		}()

		value, err := job(context.Background())
		result.Value = value
		result.Error = err

		if err != nil {
			atomic.AddInt64(&w.pool.failed, 1)
		} else {
			atomic.AddInt64(&w.pool.completed, 1)
		}
	}()
}

// cleanupLoop removes idle workers periodically
func (p *Pool[T]) cleanupLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.cleanupIdleWorkers()
		case <-p.stopCh:
			return
		}
	}
}

// cleanupIdleWorkers removes workers that have been idle too long
func (p *Pool[T]) cleanupIdleWorkers() {
	now := time.Now().UnixNano()
	maxIdleNanos := p.maxIdleTime.Nanoseconds()

	// Check idle workers and remove old ones
	availableWorkers := len(p.workerCh)
	for i := 0; i < availableWorkers; i++ {
		select {
		case w := <-p.workerCh:
			lastUsed := atomic.LoadInt64(&w.lastUsed)
			if now-lastUsed > maxIdleNanos {
				// Worker is too old, don't return it to pool
				// It will exit when it tries to return
				continue
			}
			// Worker is still fresh, return to pool
			p.workerCh <- w
		default:
			// No more workers to check
			return
		}
	}
}
