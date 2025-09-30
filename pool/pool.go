package pool

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Task represents a unit of work
type Task func()

// Pool is a simple, fast worker pool
type Pool struct {
	// Configuration (immutable after creation)
	capacity       int32
	expiryDuration time.Duration
	nonblocking    bool

	// Worker management
	workers chan *worker
	running int32

	// State management
	state int32 // 0: closed, 1: running

	// Statistics
	submitted uint64
	completed uint64

	// Cleanup
	stopCleanup chan struct{}
	wg          sync.WaitGroup
}

// worker executes tasks
type worker struct {
	pool     *Pool
	task     chan Task
	lastUsed time.Time
}

// Pool State
const (
	StateClosed int32 = iota
	StateRunning
)

// NewPool creates a new worker pool
func NewPool(size int, opts ...Option) (*Pool, error) {
	if size <= 0 {
		size = runtime.GOMAXPROCS(0)
	}

	// Load options
	options := DefaultOptions()
	for _, opt := range opts {
		opt(&options)
	}

	p := &Pool{
		capacity:       int32(size),
		expiryDuration: options.ExpiryDuration,
		nonblocking:    options.Nonblocking,
		workers:        make(chan *worker, size),
		stopCleanup:    make(chan struct{}),
	}

	// Pre-allocate workers if requested
	if options.PreAlloc {
		for i := 0; i < size; i++ {
			w := &worker{
				pool:     p,
				task:     make(chan Task, 1),
				lastUsed: time.Now(),
			}
			p.workers <- w
			w.start()
		}
	}

	// Start cleanup goroutine
	if !options.DisablePurge {
		p.wg.Add(1)
		go p.cleanupLoop()
	}

	return p, nil
}

// Submit submits a task to the pool
func (p *Pool) Submit(task Task) error {
	if task == nil {
		return ErrTaskNil
	}

	if p.IsClosed() {
		return ErrPoolClosed
	}

	// Try to get a worker
	w := p.getWorker()
	if w == nil {
		return ErrPoolOverload
	}

	// Send task to worker
	select {
	case w.task <- task:
		atomic.AddUint64(&p.submitted, 1)
		return nil
	case <-time.After(time.Second):
		// Put worker back and return error
		p.putWorker(w)
		return ErrPoolOverload
	}
}

// Running returns number of running workers
func (p *Pool) Running() int {
	return int(atomic.LoadInt32(&p.running))
}

// Free returns number of available worker slots
func (p *Pool) Free() int {
	return int(p.capacity) - p.Running()
}

// Cap returns pool capacity
func (p *Pool) Cap() int {
	return int(p.capacity)
}

// Submitted returns total submitted tasks
func (p *Pool) Submitted() uint64 {
	return atomic.LoadUint64(&p.submitted)
}

// Completed returns total completed tasks
func (p *Pool) Completed() uint64 {
	return atomic.LoadUint64(&p.completed)
}

// Tune changes pool capacity
func (p *Pool) Tune(size int) {
	if size > 0 {
		atomic.StoreInt32(&p.capacity, int32(size))
	}
}

// IsClosed returns whether pool is closed
func (p *Pool) IsClosed() bool {
	return atomic.LoadInt32(&p.state) == StateClosed
}

// Release closes the pool gracefully
func (p *Pool) Release() {
	if !atomic.CompareAndSwapInt32(&p.state, StateRunning, StateClosed) {
		return // Already closed
	}

	// Stop cleanup
	close(p.stopCleanup)

	// Close all worker channels
	close(p.workers)

	// Drain worker channel and close all workers
	for w := range p.workers {
		close(w.task)
	}

	// Wait for cleanup goroutine
	p.wg.Wait()
}

// ReleaseTimeout closes pool with timeout
func (p *Pool) ReleaseTimeout(timeout time.Duration) error {
	if p.IsClosed() {
		return ErrPoolClosed
	}

	// Mark as closed
	if !atomic.CompareAndSwapInt32(&p.state, StateRunning, StateClosed) {
		return ErrPoolClosed
	}

	// Stop cleanup
	close(p.stopCleanup)

	// Wait for workers to finish with timeout
	done := make(chan struct{})
	go func() {
		// Wait for all running tasks to complete
		for p.Running() > 0 {
			time.Sleep(10 * time.Millisecond)
		}
		close(done)
	}()

	select {
	case <-done:
		// All workers finished
		close(p.workers)
		for w := range p.workers {
			close(w.task)
		}
		p.wg.Wait()
		return nil
	case <-time.After(timeout):
		// Timeout - force close
		close(p.workers)
		for w := range p.workers {
			close(w.task)
		}
		p.wg.Wait()
		return ErrReleaseTimeout
	}
}

// Reboot restarts a closed pool
func (p *Pool) Reboot() {
	if atomic.CompareAndSwapInt32(&p.state, StateClosed, StateRunning) {
		atomic.StoreInt32(&p.running, 0)
		atomic.StoreUint64(&p.submitted, 0)
		atomic.StoreUint64(&p.completed, 0)
		p.workers = make(chan *worker, p.capacity)
		p.stopCleanup = make(chan struct{})

		// Restart cleanup
		p.wg.Add(1)
		go p.cleanupLoop()
	}
}

// getWorker retrieves or creates a worker
func (p *Pool) getWorker() *worker {
	// Fast path: try to get existing worker
	select {
	case w := <-p.workers:
		return w
	default:
	}

	// Check if we can create new worker
	running := atomic.LoadInt32(&p.running)
	if running >= p.capacity {
		if p.nonblocking {
			return nil
		}

		// Create new worker
		atomic.AddInt32(&p.running, 1)
		w := &worker{
			pool:     p,
			task:     make(chan Task, 1),
			lastUsed: time.Now(),
		}
		w.start()
		return w

	}

	// Blocking mode: wait for available worker
	select {
	case w := <-p.workers:
		return w
	case <-time.After(time.Second):
		return nil
	}

}

// putWorker returns a worker to the pool
func (p *Pool) putWorker(w *worker) {
	if p.IsClosed() {
		atomic.AddInt32(&p.running, -1)
		close(w.task)
		return
	}

	w.lastUsed = time.Now()

	// Try to return worker to pool
	select {
	case p.workers <- w:
		// Successfully returned
	default:
		// Pool is full, terminate worker
		atomic.AddInt32(&p.running, -1)
		close(w.task)
	}
}

// cleanupLoop periodically removes expired workers
func (p *Pool) cleanupLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.expiryDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.cleanExpiredWorkers()
		case <-p.stopCleanup:
			return
		}
	}
}

// cleanExpiredWorkers removes workers that have been idle too long
func (p *Pool) cleanExpiredWorkers() {
	// Only clean if we have idle workers
	if len(p.workers) == 0 {
		return
	}

	expiryTime := time.Now().Add(-p.expiryDuration)

	// Check workers in the channel
	for i := 0; i < len(p.workers); i++ {
		select {
		case w := <-p.workers:
			if w.lastUsed.Before(expiryTime) && p.Running() > 1 {
				// Expire this worker
				atomic.AddInt32(&p.running, -1)
				close(w.task)
			} else {
				// Keep this worker
				select {
				case p.workers <- w:
				default:
					// Channel full, close worker
					atomic.AddInt32(&p.running, -1)
					close(w.task)
				}
			}
		default:
			return
		}
	}
}

// start begins the worker's execution loop
func (w *worker) start() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// Panic recovery - just continue
			}
		}()

		for task := range w.task {
			if task == nil {
				continue
			}

			// Execute task with panic recovery
			func() {
				defer func() {
					if r := recover(); r != nil {
						// Task panicked, but worker continues
					}
				}()
				task()
			}()

			atomic.AddUint64(&w.pool.completed, 1)

			// Return worker to pool
			w.pool.putWorker(w)
		}
	}()
}
