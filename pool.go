package flock

import (
	"fmt"
	"runtime"
	"runtime/debug"
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

// metricsShard reduces contention by sharding metrics across cache lines
type metricsShard struct {
	_         [56]byte // Padding
	submitted uint64   // atomic
	_         [56]byte
	completed uint64 // atomic
	_         [56]byte
	dropped   uint64 // atomic
	_         [56]byte
	rejected  uint64 // atomic
	_         [56]byte
	fallback  uint64 // atomic
	_         [56]byte
}

const numMetricShards = 16

// Pool is a high-performance worker pool with hybrid queue architecture
type Pool struct {
	config  Config
	workers []*worker

	// Lifecycle management
	state      atomic.Value // PoolState
	wg         sync.WaitGroup
	submitWg   sync.WaitGroup
	fallbackWg sync.WaitGroup

	// Worker selection
	nextWorkerId uint64

	// Sharded metrics to reduce contention
	metricsShards [numMetricShards]metricsShard

	// Hybrid queue: global fallback for high contention
	globalQueue chan func()

	// Latency tracking
	latencySum   uint64 // atomic
	latencyCount uint64 // atomic
	latencyMax   uint64 // atomic
}

// NewPool creates a new worker pool with the given options.
func NewPool(opts ...Option) (*Pool, error) {
	cfg := defaultConfig()

	for _, opt := range opts {
		opt(&cfg)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	// Global queue size = (total capacity) / 4
	// This provides a good buffer for contention without excessive memory
	globalQueueSize := (cfg.NumWorkers * cfg.QueueSizePerWorker) / 4
	if globalQueueSize < 256 {
		globalQueueSize = 256 // Minimum size
	}

	pool := &Pool{
		config:      cfg,
		workers:     make([]*worker, cfg.NumWorkers),
		globalQueue: make(chan func(), globalQueueSize),
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

// Submit submits a task to the pool.
func (p *Pool) Submit(task func()) error {
	if task == nil {
		return ErrNilTask
	}

	state := p.state.Load().(PoolState)
	if state == poolStateStopped || state == poolStateDraining {
		return ErrPoolShutdown
	}

	p.submitWg.Add(1)

	// Use sharded metrics to reduce contention
	shardIdx := fastrand() % numMetricShards
	atomic.AddUint64(&p.metricsShards[shardIdx].submitted, 1)

	if err := p.trySubmit(task); err != nil {
		p.submitWg.Done()

		if err == ErrQueueFull {
			shardIdx := fastrand() % numMetricShards
			atomic.AddUint64(&p.metricsShards[shardIdx].rejected, 1)
		}

		return err
	}

	return nil
}

// trySubmit attempts to submit using hybrid approach
func (p *Pool) trySubmit(task func()) error {
	numWorkers := len(p.workers)

	// Round-robin starting point (still useful for load distribution)
	next := atomic.AddUint64(&p.nextWorkerId, 1)
	startIdx := int(next % uint64(numWorkers))

	switch p.config.BlockingStrategy {
	case BlockWhenQueueFull:
		return p.submitBlocking(task, startIdx, numWorkers)

	case ErrorWhenQueueFull:
		return p.submitNonBlocking(task, startIdx, numWorkers)

	case NewThreadWhenQueueFull:
		return p.submitWithFallback(task, startIdx, numWorkers)
	}

	return nil
}

// submitBlocking tries local queues, then global, then blocks
func (p *Pool) submitBlocking(task func(), startIdx, numWorkers int) error {
	for p.state.Load().(PoolState) == poolStateRunning {
		// Phase 1: Try all local worker queues (fast path)
		for i := 0; i < numWorkers; i++ {
			idx := (startIdx + i) % numWorkers
			if p.workers[idx].queue.tryPush(task) {
				p.workers[idx].signal()
				return nil
			}
		}

		// Phase 2: Try global queue (contention-optimized path)
		select {
		case p.globalQueue <- task:
			shardIdx := fastrand() % numMetricShards
			atomic.AddUint64(&p.metricsShards[shardIdx].fallback, 1)
			return nil
		default:
			// Global queue also full, yield and retry
			runtime.Gosched()
		}
	}

	return ErrPoolShutdown
}

// submitNonBlocking tries local queues, then global, then returns error
func (p *Pool) submitNonBlocking(task func(), startIdx, numWorkers int) error {
	// Phase 1: Try all local worker queues
	for i := 0; i < numWorkers; i++ {
		idx := (startIdx + i) % numWorkers
		if p.workers[idx].queue.tryPush(task) {
			p.workers[idx].signal()
			return nil
		}
	}

	// Phase 2: Try global queue
	select {
	case p.globalQueue <- task:
		shardIdx := fastrand() % numMetricShards
		atomic.AddUint64(&p.metricsShards[shardIdx].fallback, 1)
		return nil
	default:
		return ErrQueueFull
	}
}

// submitWithFallback tries local, then global, then spawns goroutine
func (p *Pool) submitWithFallback(task func(), startIdx, numWorkers int) error {
	// Phase 1: Try all local worker queues
	for i := 0; i < numWorkers; i++ {
		idx := (startIdx + i) % numWorkers
		if p.workers[idx].queue.tryPush(task) {
			p.workers[idx].signal()
			return nil
		}
	}

	// Phase 2: Try global queue
	select {
	case p.globalQueue <- task:
		shardIdx := fastrand() % numMetricShards
		atomic.AddUint64(&p.metricsShards[shardIdx].fallback, 1)
		return nil
	default:
		// Phase 3: Spawn new goroutine
		shardIdx := fastrand() % numMetricShards
		atomic.AddUint64(&p.metricsShards[shardIdx].fallback, 1)

		p.fallbackWg.Add(1)
		go func() {
			defer p.fallbackWg.Done()

			if p.state.Load().(PoolState) != poolStateRunning {
				shardIdx := fastrand() % numMetricShards
				atomic.AddUint64(&p.metricsShards[shardIdx].dropped, 1)
				p.submitWg.Done()
				return
			}

			startTime := time.Now()
			p.execute(task)
			p.recordLatency(time.Since(startTime))
		}()
		return nil
	}
}

// Shutdown stops the pool
func (p *Pool) Shutdown(graceful bool) {
	if graceful {
		if !p.state.CompareAndSwap(poolStateRunning, poolStateDraining) {
			return
		}

		// Close global queue to signal workers
		close(p.globalQueue)

		// Wait for workers to drain queues
		p.wg.Wait()
		// Wait for fallback goroutines to exit
		p.fallbackWg.Wait()

		p.state.Store(poolStateStopped)
	} else {
		currentState := p.state.Load().(PoolState)
		if currentState == poolStateStopped {
			return
		}
		p.state.Store(poolStateStopped)

		// Close global queue
		close(p.globalQueue)

		// Wake up all parked workers
		for _, wk := range p.workers {
			wk.signal()
		}

		// Wait for workers to exit
		p.wg.Wait()
		// Wait for fallback goroutines to exit
		p.fallbackWg.Wait()
	}
}

// Wait blocks until all submitted tasks have completed
func (p *Pool) Wait() {
	p.submitWg.Wait()
}

// Stats returns a snapshot of pool statistics
func (p *Pool) Stats() Stats {
	// Aggregate sharded metrics
	var submitted, completed, dropped, rejected, fallback uint64
	for i := 0; i < numMetricShards; i++ {
		submitted += atomic.LoadUint64(&p.metricsShards[i].submitted)
		completed += atomic.LoadUint64(&p.metricsShards[i].completed)
		dropped += atomic.LoadUint64(&p.metricsShards[i].dropped)
		rejected += atomic.LoadUint64(&p.metricsShards[i].rejected)
		fallback += atomic.LoadUint64(&p.metricsShards[i].fallback)
	}

	inFlight := submitted - completed - dropped - rejected

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
			State:         wk.getState().string(),
		}
	}

	// Add global queue to totals
	globalQueueLen := len(p.globalQueue)
	globalQueueCap := cap(p.globalQueue)
	totalDepth += int64(globalQueueLen)
	totalCapacity += int64(globalQueueCap)

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
		Dropped:            dropped,
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

// IsShutdown returns true if the pool has been shut down
func (p *Pool) IsShutdown() bool {
	return p.state.Load().(PoolState) == poolStateStopped
}

// NumWorkers returns the number of workers in the pool
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
			if p.config.PanicHandler != nil {
				p.config.PanicHandler(r)
			} else {
				stackTrace := debug.Stack()
				err := PoolError{msg: "Error running task in fallback goroutine", err: fmt.Errorf("%v\n%s", r, stackTrace)}
				fmt.Printf("[flock error] %v\n", err)
			}
		}

		shardIdx := fastrand() % numMetricShards
		atomic.AddUint64(&p.metricsShards[shardIdx].completed, 1)
		p.submitWg.Done()
	}()

	task()
}

var rngSeed uint32 = 1

// fastrand provides fast random number generation
func fastrand() uint32 {
	// Xorshift RNG - fast and good enough for load distribution
	seed := atomic.LoadUint32(&rngSeed)
	seed ^= seed << 13
	seed ^= seed >> 17
	seed ^= seed << 5
	atomic.StoreUint32(&rngSeed, seed)
	return seed
}
