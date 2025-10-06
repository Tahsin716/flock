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
	state atomic.Value // PoolState
	wg    sync.WaitGroup

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
	stolen    uint64 // atomic
	fallback  uint64 // atomic
}

// New creates a new worker pool
func NewPool(config Config) (*Pool, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Apply defaults
	if config.NumWorkers == 0 {
		config.NumWorkers = runtime.NumCPU()
	}
	if config.QueueSizePerWorker == 0 {
		config.QueueSizePerWorker = 256
	}
	if config.MaxParkTime == 0 {
		config.MaxParkTime = DefaultConfig().MaxParkTime
	}
	if config.SpinCount == 0 {
		config.SpinCount = DefaultConfig().SpinCount
	}

	pool := &Pool{
		config:  config,
		workers: make([]*Worker, config.NumWorkers),
	}
	pool.state.Store(PoolStateRunning)

	// Initialize workers
	for i := 0; i < config.NumWorkers; i++ {
		// Ensure external queue size is power of 2
		extSize := config.QueueSizePerWorker
		pool.workers[i] = newWorker(i, pool, extSize, int64(config.QueueSizePerWorker))
	}

	// Start workers
	for i := 0; i < config.NumWorkers; i++ {
		pool.wg.Add(1)
		worker := pool.workers[i]
		go func() {
			defer pool.wg.Done()
			worker.run()
		}()
	}

	return pool, nil
}

// Submit never fails - executes in caller's goroutine if queues full
func (p *Pool) Submit(task func()) {
	if task == nil {
		return
	}

	state := p.state.Load().(PoolState)
	if state == PoolStateStopped {
		return
	}

	atomic.AddUint64(&p.metrics.submitted, 1)

	if p.tryFastSubmit(task) {
		return
	}

	// Fallback: execute in caller's goroutine
	atomic.AddUint64(&p.metrics.fallback, 1)
	startTime := time.Now()
	task()
	p.recordLatency(time.Since(startTime))
	atomic.AddUint64(&p.metrics.completed, 1)
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

		if worker.externalTasks.TryPush(task) {
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

// SubmitWithTimeout submits with timeout
func (p *Pool) SubmitWithTimeout(timeout time.Duration, task func()) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	p.SubmitWithContext(ctx, task)
}

// TrySubmitWithTimeout is TrySubmit with timeout
func (p *Pool) TrySubmitWithTimeout(timeout time.Duration, task func()) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return p.TrySubmitWithContext(ctx, task)
}

// Shutdown stops the pool
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

// ShutdownWithTimeout shuts down with timeout
func (p *Pool) ShutdownWithTimeout(timeout time.Duration, graceful bool) error {
	if graceful {
		if !p.state.CompareAndSwap(PoolStateRunning, PoolStateDraining) {
			return nil
		}
	} else {
		p.state.Store(PoolStateStopped)
		for _, worker := range p.workers {
			worker.signal()
		}
	}

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		p.state.Store(PoolStateStopped)
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return ErrTimeout
	}
}

// Wait blocks until all tasks complete
func (p *Pool) Wait() {
	backoff := time.Microsecond

	for {
		submitted := atomic.LoadUint64(&p.metrics.submitted)
		completed := atomic.LoadUint64(&p.metrics.completed)
		rejected := atomic.LoadUint64(&p.metrics.rejected)

		if completed+rejected >= submitted {
			return
		}

		time.Sleep(backoff)
		if backoff < 10*time.Millisecond {
			backoff *= 2
		}
	}
}

// WaitWithTimeout waits with timeout
func (p *Pool) WaitWithTimeout(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	backoff := time.Microsecond

	for {
		submitted := atomic.LoadUint64(&p.metrics.submitted)
		completed := atomic.LoadUint64(&p.metrics.completed)
		rejected := atomic.LoadUint64(&p.metrics.rejected)

		if completed+rejected >= submitted {
			return nil
		}

		if time.Now().After(deadline) {
			return ErrTimeout
		}

		time.Sleep(backoff)
		if backoff < 10*time.Millisecond {
			backoff *= 2
		}
	}
}

// Stats returns pool statistics
func (p *Pool) Stats() Stats {
	submitted := atomic.LoadUint64(&p.metrics.submitted)
	completed := atomic.LoadUint64(&p.metrics.completed)
	rejected := atomic.LoadUint64(&p.metrics.rejected)
	stolen := atomic.LoadUint64(&p.metrics.stolen)
	fallback := atomic.LoadUint64(&p.metrics.fallback)
	inFlight := submitted - completed - rejected

	workerStats := make([]WorkerStats, len(p.workers))
	totalDepth := int64(0)
	totalCapacity := int64(0)

	for i, worker := range p.workers {
		extDepth := worker.externalTasks.Size()
		localDepth := int(worker.localWork.Size())
		extCap := worker.externalTasks.Capacity()
		localCap := int(worker.localWork.Capacity())

		totalDepth += int64(extDepth + localDepth)
		totalCapacity += int64(extCap + localCap)

		workerStats[i] = WorkerStats{
			WorkerID:         i,
			TasksExecuted:    atomic.LoadUint64(&worker.tasksExecuted),
			TasksStolen:      atomic.LoadUint64(&worker.tasksStolen),
			ExternalDepth:    extDepth,
			LocalDepth:       localDepth,
			ExternalCapacity: extCap,
			LocalCapacity:    localCap,
			State:            worker.getState(),
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
		Stolen:             stolen,
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
		WorkerID:         workerID,
		TasksExecuted:    atomic.LoadUint64(&worker.tasksExecuted),
		TasksStolen:      atomic.LoadUint64(&worker.tasksStolen),
		ExternalDepth:    worker.externalTasks.Size(),
		LocalDepth:       int(worker.localWork.Size()),
		ExternalCapacity: worker.externalTasks.Capacity(),
		LocalCapacity:    int(worker.localWork.Capacity()),
		State:            worker.getState(),
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

// fastrand returns a pseudorandom uint32
//
//go:linkname fastrand runtime.fastrand
func fastrand() uint32
