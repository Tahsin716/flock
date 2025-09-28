package pool

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// FastPool is a high-performance, fixed-size pool for fire-and-forget tasks.
// It uses a context for lifecycle management and a single channel for tasks.
type FastPool struct {
	tasks    chan func()
	capacity int32

	// Statistics
	submitted int64
	running   int64
	completed int64
	failed    int64

	// Lifecycle
	closeOnce sync.Once
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// NewFast creates a fixed-size pool for fire-and-forget tasks.
func NewFast(size int) *FastPool {
	if size <= 0 {
		size = runtime.GOMAXPROCS(0)
	}

	ctx, cancel := context.WithCancel(context.Background())

	p := &FastPool{
		tasks:    make(chan func(), size),
		capacity: int32(size),
		ctx:      ctx,
		cancel:   cancel,
	}

	// Start workers
	p.wg.Add(size)
	for i := 0; i < size; i++ {
		go p.workerLoop()
	}

	return p
}

// Submit enqueues a task, blocking if the queue is full.
func (p *FastPool) Submit(task func()) error {
	// First, check if the pool is closed to prevent a race with Close().
	if p.ctx.Err() != nil {
		return ErrPoolClosed
	}

	select {
	case p.tasks <- task:
		atomic.AddInt64(&p.submitted, 1)
		return nil
	case <-p.ctx.Done():
		return ErrPoolClosed
	}
}

// TrySubmit attempts to enqueue a task without blocking.
func (p *FastPool) TrySubmit(task func()) bool {
	// First, check if the pool is closed to prevent a race with Close().
	if p.ctx.Err() != nil {
		return false
	}

	select {
	case p.tasks <- task:
		atomic.AddInt64(&p.submitted, 1)
		return true
	default:
		return false
	}
}

// Close shuts down the pool gracefully, ensuring all submitted tasks are executed.
func (p *FastPool) Close() {
	p.closeOnce.Do(func() {
		p.cancel()
		close(p.tasks)
		p.wg.Wait()
	})
}

// Stats returns current statistics.
func (p *FastPool) Stats() Stats {
	return Stats{
		Submitted: atomic.LoadInt64(&p.submitted),
		Running:   atomic.LoadInt64(&p.running),
		Completed: atomic.LoadInt64(&p.completed),
		Failed:    atomic.LoadInt64(&p.failed),
	}
}

// Cap returns pool capacity.
func (p *FastPool) Cap() int {
	return int(p.capacity)
}

// workerLoop is the main execution loop for a worker. It exits when the tasks
// channel is closed and drained.
func (p *FastPool) workerLoop() {
	defer p.wg.Done()
	for task := range p.tasks {
		p.executeTask(task)
	}
}

// executeTask runs a single task with panic recovery and statistics updates.
func (p *FastPool) executeTask(task func()) {
	atomic.AddInt64(&p.running, 1)
	defer atomic.AddInt64(&p.running, -1)

	defer func() {
		if r := recover(); r != nil {
			atomic.AddInt64(&p.failed, 1)
			fmt.Printf("workpool: panic recovered in FastPool: %v\n%s\n", r, debug.Stack())
		} else {
			atomic.AddInt64(&p.completed, 1)
		}
	}()

	task()
}
