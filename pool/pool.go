package pool

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
)

// Task is a unit of work
type Task func()

// Core Pool minimal in design
type Pool struct {
	// Semaphore pattern: simple and effective
	slots chan struct{}

	// Worker coordination
	wg sync.WaitGroup

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once

	// Stats
	submitted atomic.Uint64
	completed atomic.Uint64
}

// New creates a pool with given capacity
func New(capacity int) *Pool {
	if capacity <= 0 {
		capacity = runtime.GOMAXPROCS(0)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Pool{
		slots:  make(chan struct{}, capacity),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Go submits a task (like goroutine but limited)
func (p *Pool) Go(task Task) error {
	if task == nil {
		return ErrTaskNil
	}

	// Acquire slot (blocks if full)
	select {
	case p.slots <- struct{}{}:
		p.submitted.Add(1)
		p.wg.Add(1)

		go func() {
			defer func() {
				<-p.slots // Release slot
				p.wg.Done()
				p.completed.Add(1)

				if r := recover(); r != nil {
					// Recover from panics
				}
			}()

			task()
		}()

		return nil

	case <-p.ctx.Done():
		return ErrPoolClosed
	}
}

// TryGo attempts non-blocking submission
func (p *Pool) TryGo(task Task) bool {
	if task == nil {
		return false
	}

	select {
	case p.slots <- struct{}{}:
		p.submitted.Add(1)
		p.wg.Add(1)

		go func() {
			defer func() {
				<-p.slots
				p.wg.Done()
				p.completed.Add(1)

				if r := recover(); r != nil {
					// Recover
				}
			}()

			task()
		}()

		return true

	default:
		return false
	}
}

// Wait waits for all submitted tasks to complete
func (p *Pool) Wait() {
	p.wg.Wait()
}

// Close shuts down the pool
func (p *Pool) Close() error {
	p.once.Do(func() {
		p.cancel()
	})
	return nil
}

// Stats returns pool statistics
func (p *Pool) Stats() (submitted, completed uint64) {
	return p.submitted.Load(), p.completed.Load()
}

// Cap returns pool capacity
func (p *Pool) Cap() int {
	return cap(p.slots)
}

// Running returns approximate number of running tasks
func (p *Pool) Running() int {
	return len(p.slots)
}
