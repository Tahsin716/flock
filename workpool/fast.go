package workpool

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// FastPool is an ultra-high-performance pool for fire-and-forget functions
type FastPool struct {
	workers  []*fastWorker
	workerCh chan *fastWorker
	capacity int32

	// Statistics
	submitted int64
	running   int64

	// Lifecycle
	closed int64
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// fastWorker is optimized for minimal overhead
type fastWorker struct {
	taskCh chan func()
	pool   *FastPool
}

// NewFast creates an ultra-fast pool for fire-and-forget tasks
func NewFast(size int) *FastPool {
	if size <= 0 {
		size = runtime.GOMAXPROCS(0) * 4
	}

	p := &FastPool{
		workers:  make([]*fastWorker, 0, size),
		workerCh: make(chan *fastWorker, size),
		capacity: int32(size),
		stopCh:   make(chan struct{}),
	}

	// Pre-allocate all workers
	for i := 0; i < size; i++ {
		w := &fastWorker{
			taskCh: make(chan func(), 1),
			pool:   p,
		}
		p.workers = append(p.workers, w)
		p.workerCh <- w

		// Start worker goroutine
		p.wg.Add(1)
		go w.run()
	}

	return p
}

// Submit submits a function for execution (ants-style interface)
func (p *FastPool) Submit(task func()) error {
	if atomic.LoadInt64(&p.closed) == 1 {
		return ErrPoolClosed
	}

	select {
	case w := <-p.workerCh:
		atomic.AddInt64(&p.submitted, 1)
		w.taskCh <- task
		return nil
	default:
		return ErrPoolFull
	}
}

// TrySubmit attempts non-blocking submission
func (p *FastPool) TrySubmit(task func()) bool {
	if atomic.LoadInt64(&p.closed) == 1 {
		return false
	}

	select {
	case w := <-p.workerCh:
		atomic.AddInt64(&p.submitted, 1)
		w.taskCh <- task
		return true
	default:
		return false
	}
}

// Close shuts down the pool
func (p *FastPool) Close() {
	if atomic.CompareAndSwapInt64(&p.closed, 0, 1) {
		close(p.stopCh)
		p.wg.Wait()
	}
}

// Running returns number of active workers
func (p *FastPool) Running() int64 {
	return atomic.LoadInt64(&p.running)
}

// Free returns number of available workers
func (p *FastPool) Free() int {
	return len(p.workerCh)
}

// Cap returns pool capacity
func (p *FastPool) Cap() int {
	return int(p.capacity)
}

// fastWorker run loop - optimized for minimal overhead
func (w *fastWorker) run() {
	defer w.pool.wg.Done()

	for {
		select {
		case task := <-w.taskCh:
			// Execute task with panic recovery
			atomic.AddInt64(&w.pool.running, 1)
			func() {
				defer func() {
					atomic.AddInt64(&w.pool.running, -1)
					if recover() != nil {
						// Ignore panics in fast mode - just continue
					}
				}()
				task()
			}()

			// Return to pool immediately
			select {
			case w.pool.workerCh <- w:
			case <-w.pool.stopCh:
				return
			}

		case <-w.pool.stopCh:
			return
		}
	}
}
