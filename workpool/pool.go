package workpool

import (
	"context"
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
	}

	// Start cleanup goroutine for idle workers
	if config.CleanupInterval > 0 {
		p.wg.Add(1)
		go p.cleanupLoop()
	}

	return p
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
