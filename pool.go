package flock

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// PoolState represents pool lifecycle states
type PoolState uint32

const (
	poolStateRunning PoolState = iota
	poolStateDraining
	poolStateStopped
)

// Pool is a high-performance worker pool
type Pool struct {
	config  Config
	workers []*worker

	// Lifecycle management
	state    atomic.Value // PoolState
	wg       sync.WaitGroup
	submitWg sync.WaitGroup

	// worker id for round-robin distribution of tasks
	nextWorkerId uint64

	// Metrics
	metrics poolMetrics

	// Latency tracking
	latencySum   uint64 // atomic
	latencyCount uint64 // atomic
	latencyMax   uint64 // atomic
}

// poolMetrics tracks pool-wide statistics
type poolMetrics struct {
	submitted uint64 // atomic
	completed uint64 // atomic
	rejected  uint64 // atomic
	fallback  uint64 // atomic
}

// NewPool creates a new worker pool with the given options.
// It returns an error if the configuration is invalid.
//
// Example:
//
//	pool, err := flock.NewPool(
//	    flock.WithNumWorkers(4),
//	    flock.WithQueueSizePerWorker(256),
//	)
func NewPool(opts ...Option) (*Pool, error) {
	// Start with default config
	cfg := defaultConfig()

	// Apply user options
	for _, opt := range opts {
		opt(&cfg)
	}

	// validate final configuration
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	pool := &Pool{
		config:  cfg,
		workers: make([]*worker, cfg.NumWorkers),
	}
	pool.state.Store(poolStateRunning)

	// Initialize and start workers
	for i := 0; i < cfg.NumWorkers; i++ {
		pool.workers[i] = newWorker(i, pool, cfg.QueueSizePerWorker)
	}

	for _, w := range pool.workers {
		pool.wg.Add(1)
		go func(wk *worker) {
			defer pool.wg.Done()
			wk.run()
		}(w)
	}

	return pool, nil
}

// Submit submits a task to the pool. It never returns an error for queue full -
// instead it executes the task in the caller's goroutine if all worker queues are full.
//
// Returns ErrNilTask if task is nil.
// Returns ErrPoolShutdown if the pool has been shut down.
//
// Example:
//
//	err := pool.Submit(func() {
//	    fmt.Println("Task executed")
//	})
func (p *Pool) Submit(task func()) error {
	if task == nil {
		return ErrNilTask
	}

	state := p.state.Load().(PoolState)
	if state == poolStateStopped {
		return ErrPoolShutdown
	}

	// Add to waitgroup BEFORE incrementing submitted to prevent races
	p.submitWg.Add(1)
	atomic.AddUint64(&p.metrics.submitted, 1)

	if p.tryFastSubmit(task) {
		return nil
	}

	// Fallback: execute in caller's goroutine
	atomic.AddUint64(&p.metrics.fallback, 1)
	startTime := time.Now()
	p.execute(task)
	p.recordLatency(time.Since(startTime))
	atomic.AddUint64(&p.metrics.completed, 1)

	return nil
}

// tryFastSubmit attempts to submit to worker queues via MPSC
func (p *Pool) tryFastSubmit(task func()) bool {
	numWorkers := len(p.workers)

	// Round-robin start index
	next := atomic.AddUint64(&p.nextWorkerId, 1)
	startIdx := int(next % uint64(numWorkers))

	for i := 0; i < numWorkers; i++ {
		idx := (startIdx + i) % numWorkers
		wk := p.workers[idx]

		if wk.queue.tryPush(task) {
			wk.signal()
			return true
		}
	}

	return false
}

// Shutdown stops the pool. If graceful is true, it waits for all queued tasks
// to complete. If false, it stops immediately after current tasks finish.
//
// Multiple calls to Shutdown are safe and will be ignored after the first call.
//
// Example:
//
//	pool.Shutdown(true)  // Wait for all tasks
//	pool.Shutdown(false) // Stop immediately
func (p *Pool) Shutdown(graceful bool) {
	if graceful {
		if !p.state.CompareAndSwap(poolStateRunning, poolStateDraining) {
			return
		}
	} else {
		currentState := p.state.Load().(PoolState)
		if currentState == poolStateStopped {
			return
		}
		p.state.Store(poolStateStopped)

		for _, wk := range p.workers {
			wk.signal()
		}
	}

	p.wg.Wait()
	p.state.Store(poolStateStopped)
}

// Wait blocks until all submitted tasks have completed.
// It does not shut down the pool - the pool remains available for new tasks.
//
// Example:
//
//	pool.Submit(task1)
//	pool.Submit(task2)
//	pool.Wait() // Blocks until task1 and task2 complete
func (p *Pool) Wait() {
	p.submitWg.Wait()
}

// Stats returns a snapshot of pool statistics including task counts,
// latency metrics, and per-worker statistics.
//
// Note: Stats are collected without locks, so values may be slightly
// inconsistent during concurrent operations.
//
// Example:
//
//	stats := pool.Stats()
//	fmt.Printf("Completed: %d/%d\n", stats.Completed, stats.Submitted)
func (p *Pool) Stats() Stats {
	submitted := atomic.LoadUint64(&p.metrics.submitted)
	completed := atomic.LoadUint64(&p.metrics.completed)
	rejected := atomic.LoadUint64(&p.metrics.rejected)
	fallback := atomic.LoadUint64(&p.metrics.fallback)
	inFlight := submitted - completed - rejected

	workerStats := make([]WorkerStats, len(p.workers))
	totalDepth := int64(0)
	totalCapacity := int64(0)

	for i, wk := range p.workers {
		depth := wk.queue.size()
		capacity := wk.queue.capacity()

		totalDepth += int64(depth)
		totalCapacity += int64(capacity)

		workerStats[i] = WorkerStats{
			WorkerID:      i,
			TasksExecuted: atomic.LoadUint64(&wk.tasksExecuted),
			TasksFailed:   atomic.LoadUint64(&wk.tasksFailed),
			Capacity:      capacity,
			State:         wk.getState(),
		}
	}

	utilization := float64(0)
	if totalCapacity > 0 {
		utilization = float64(totalDepth) / float64(totalCapacity) * 100.0
	}

	latencyCount := atomic.LoadUint64(&p.latencyCount)
	latencyAvg := time.Duration(0)
	latencyMax := time.Duration(0)

	if latencyCount > 0 {
		latencySum := atomic.LoadUint64(&p.latencySum)
		latencyAvg = time.Duration(latencySum/latencyCount) * time.Microsecond
		latencyMax = time.Duration(atomic.LoadUint64(&p.latencyMax)) * time.Microsecond
	}

	return Stats{
		Submitted:          submitted,
		Completed:          completed,
		Rejected:           rejected,
		FallbackExecuted:   fallback,
		InFlight:           inFlight,
		Utilization:        utilization,
		WorkerStats:        workerStats,
		NumWorkers:         len(p.workers),
		TotalQueueDepth:    int(totalDepth),
		TotalQueueCapacity: int(totalCapacity),
		LatencyAvg:         latencyAvg,
		LatencyMax:         latencyMax,
	}
}

// IsShutdown returns true if the pool has been shut down.
//
// Example:
//
//	if pool.IsShutdown() {
//	    fmt.Println("Pool is no longer accepting tasks")
//	}
func (p *Pool) IsShutdown() bool {
	return p.state.Load().(PoolState) == poolStateStopped
}

// NumWorkers returns the number of workers in the pool.
//
// Example:
//
//	fmt.Printf("Pool has %d workers\n", pool.NumWorkers())
func (p *Pool) NumWorkers() int {
	return len(p.workers)
}

// recordLatency records task execution latency
func (p *Pool) recordLatency(duration time.Duration) {
	micros := uint64(duration.Microseconds())

	atomic.AddUint64(&p.latencySum, micros)
	atomic.AddUint64(&p.latencyCount, 1)

	for {
		current := atomic.LoadUint64(&p.latencyMax)
		if micros <= current {
			break
		}
		if atomic.CompareAndSwapUint64(&p.latencyMax, current, micros) {
			break
		}
	}
}

// execute runs a task with panic recovery
func (p *Pool) execute(task func()) {
	defer func() {
		if r := recover(); r != nil {
			atomic.AddUint64(&p.workers[0].tasksFailed, 1) // Track panic
			if p.config.PanicHandler != nil {
				p.config.PanicHandler(r)
			} else {
				// Default: capture stack trace silently
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false)
				_ = n // Stack trace in buf[:n]
			}
		}

		p.submitWg.Done()
	}()

	// Execute the task
	task()
}
