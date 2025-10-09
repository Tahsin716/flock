package flock

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	_ "unsafe" // for go:linkname
)

// PoolState represents pool lifecycle states
type PoolState uint32

const (
	PoolStateRunning PoolState = iota
	PoolStateDraining
	PoolStateStopped
)

// Pool is a high-performance worker pool with work stealing
type Pool struct {
	config  Config
	workers []*Worker

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

// NewPool creates a new worker pool
func NewPool(opts ...Option) (*Pool, error) {
	// Start with default config
	cfg := DefaultConfig()

	// Apply user options
	for _, opt := range opts {
		opt(&cfg)
	}

	// Validate final configuration
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	pool := &Pool{
		config:  cfg,
		workers: make([]*Worker, cfg.NumWorkers),
	}
	pool.state.Store(PoolStateRunning)

	// Initialize and start workers
	for i := 0; i < cfg.NumWorkers; i++ {
		pool.workers[i] = newWorker(i, pool, cfg.QueueSizePerWorker)
	}

	for _, w := range pool.workers {
		pool.wg.Add(1)
		go func(worker *Worker) {
			defer pool.wg.Done()
			worker.run()
		}(w)
	}

	return pool, nil
}

// Submit never fails - executes in caller's goroutine if queues full
func (p *Pool) Submit(task func()) error {
	if task == nil {
		return ErrNilTask
	}

	state := p.state.Load().(PoolState)
	if state == PoolStateStopped {
		return ErrPoolShutdown
	}

	atomic.AddUint64(&p.metrics.submitted, 1)
	p.submitWg.Add(1)

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

// TrySubmit returns error if queues full
func (p *Pool) TrySubmit(task func()) error {
	if task == nil {
		return ErrInvalidConfig("task cannot be nil")
	}

	state := p.state.Load().(PoolState)
	if state == PoolStateStopped {
		return ErrPoolShutdown
	}

	atomic.AddUint64(&p.metrics.submitted, 1)

	if p.tryFastSubmit(task) {
		return nil
	}

	atomic.AddUint64(&p.metrics.rejected, 1)
	return ErrQueueFull
}

// tryFastSubmit attempts to submit to worker queues via MPSC
func (p *Pool) tryFastSubmit(task func()) bool {
	numWorkers := len(p.workers)

	// Round-robin start index
	next := atomic.AddUint64(&p.nextWorkerId, 1)
	startIdx := int(next % uint64(numWorkers))

	for i := 0; i < numWorkers; i++ {
		idx := (startIdx + i) % numWorkers
		worker := p.workers[idx]

		if worker.queue.TryPush(task) {
			worker.signal()
			return true
		}
	}

	return false
}

// SubmitWithContext submits with context support
func (p *Pool) SubmitWithContext(ctx context.Context, task func()) {
	if ctx == nil {
		p.Submit(task)
		return
	}

	select {
	case <-ctx.Done():
		return
	default:
	}

	wrapper := func() {
		if ctx.Err() != nil {
			return
		}
		task()
	}

	p.Submit(wrapper)
}

// TrySubmitWithContext is TrySubmit with context
func (p *Pool) TrySubmitWithContext(ctx context.Context, task func()) error {
	if ctx == nil {
		return p.TrySubmit(task)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	wrapper := func() {
		if ctx.Err() != nil {
			return
		}
		task()
	}

	return p.TrySubmit(wrapper)
}

// Shutdown stops the pool, and waits for all pending tasks in queue to complete
func (p *Pool) Shutdown(graceful bool) {
	if graceful {
		if !p.state.CompareAndSwap(PoolStateRunning, PoolStateDraining) {
			return
		}
	} else {
		currentState := p.state.Load().(PoolState)
		if currentState == PoolStateStopped {
			return
		}
		p.state.Store(PoolStateStopped)

		for _, worker := range p.workers {
			worker.signal()
		}
	}

	p.wg.Wait()
	p.state.Store(PoolStateStopped)
}

// Wait blocks until all tasks complete
// it does not shutdown the pool
func (p *Pool) Wait() {
	p.submitWg.Wait()
}

// Stats returns pool statistics
func (p *Pool) Stats() Stats {
	submitted := atomic.LoadUint64(&p.metrics.submitted)
	completed := atomic.LoadUint64(&p.metrics.completed)
	rejected := atomic.LoadUint64(&p.metrics.rejected)
	fallback := atomic.LoadUint64(&p.metrics.fallback)
	inFlight := submitted - completed - rejected

	workerStats := make([]WorkerStats, len(p.workers))
	totalDepth := int64(0)
	totalCapacity := int64(0)

	for i, worker := range p.workers {
		depth := worker.queue.Size()
		capacity := worker.queue.Capacity()

		totalDepth += int64(depth)
		totalCapacity += int64(capacity)

		workerStats[i] = WorkerStats{
			WorkerID:      i,
			TasksExecuted: atomic.LoadUint64(&worker.tasksExecuted),
			TasksFailed:   atomic.LoadUint64(&worker.tasksFailed),
			Capacity:      capacity,
			State:         worker.getState(),
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

// recordLatency records task latency
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

// GetWorkerStats returns stats for specific worker
func (p *Pool) GetWorkerStats(workerID int) (WorkerStats, error) {
	if workerID < 0 || workerID >= len(p.workers) {
		return WorkerStats{}, ErrInvalidConfig("invalid worker ID")
	}

	worker := p.workers[workerID]
	return WorkerStats{
		WorkerID:      workerID,
		TasksExecuted: atomic.LoadUint64(&worker.tasksExecuted),
		TasksFailed:   atomic.LoadUint64(&worker.tasksFailed),
		Capacity:      worker.queue.Capacity(),
		State:         worker.getState(),
	}, nil
}

// IsShutdown returns true if pool is shutdown
func (p *Pool) IsShutdown() bool {
	return p.state.Load().(PoolState) == PoolStateStopped
}

// NumWorkers returns number of workers
func (p *Pool) NumWorkers() int {
	return len(p.workers)
}

func (p *Pool) execute(task func()) {
	defer func() {
		if r := recover(); r != nil {
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
