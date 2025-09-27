package workpool

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// FastPool is a high-performance, fixed-size pool for fire-and-forget tasks.
type FastPool struct {
	workerCh chan func() // Task channel for all workers.
	capacity int32

	// Statistics
	submitted int64
	running   int64
	completed int64
	failed    int64

	// Lifecycle
	closeOnce sync.Once
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

// NewFast creates an ultra-fast, fixed-size pool for fire-and-forget tasks.
// Panics in submitted tasks are recovered and logged to stderr.
func NewFast(size int) *FastPool {
	if size <= 0 {
		size = runtime.GOMAXPROCS(0)
	}

	p := &FastPool{
		workerCh: make(chan func(), size),
		capacity: int32(size),
		stopCh:   make(chan struct{}),
	}

	p.wg.Add(size)
	for i := 0; i < size; i++ {
		go p.workerLoop()
	}

	return p
}

// Submit enqueues a task for execution, blocking if the pool's queue is full.
func (p *FastPool) Submit(task func()) {
	select {
	case p.workerCh <- task:
		atomic.AddInt64(&p.submitted, 1)
	case <-p.stopCh:
	}
}

// TrySubmit attempts to enqueue a task without blocking.
// It returns true if the task was submitted, and false if the pool's queue is full.
func (p *FastPool) TrySubmit(task func()) bool {
	select {
	case p.workerCh <- task:
		atomic.AddInt64(&p.submitted, 1)
		return true
	default:
		return false
	}
}

// Close gracefully shuts down the pool, waiting for all active tasks to finish.
func (p *FastPool) Close() {
	p.closeOnce.Do(func() {
		close(p.stopCh)
		p.wg.Wait()
	})
}

// Stats returns current statistics about the Pool.
func (p *FastPool) Stats() Stats {
	return Stats{
		Submitted: atomic.LoadInt64(&p.submitted),
		Running:   atomic.LoadInt64(&p.running),
		Completed: atomic.LoadInt64(&p.completed),
		Failed:    atomic.LoadInt64(&p.failed),
	}
}

// workerLoop is the main execution loop for a fast pool worker.
func (p *FastPool) workerLoop() {
	defer p.wg.Done()
	for {
		select {
		case task := <-p.workerCh:
			p.execute(task)
		case <-p.stopCh:
			// Drain any remaining tasks after the pool is closed.
			for task := range p.workerCh {
				p.execute(task)
			}
			return
		}
	}
}

// execute runs a task with panic recovery.
func (p *FastPool) execute(task func()) {
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
