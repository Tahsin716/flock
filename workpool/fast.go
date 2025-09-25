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
