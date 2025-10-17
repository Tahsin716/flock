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

// NewPool creates and initializes a new worker pool with the specified configuration.
//
// The pool starts immediately upon creation with all workers active and ready to
// accept tasks. Workers begin in a parked state and wake up as tasks arrive.
//
// Configuration is provided through functional options (Option functions). If no
// options are provided, the pool uses production-ready defaults:
//   - NumWorkers: runtime.NumCPU()
//   - QueueSizePerWorker: 256
//   - MaxParkTime: 10ms
//   - SpinCount: 30
//   - BlockingStrategy: BlockWhenQueueFull
//
// The pool allocates resources including:
//   - Worker goroutines (NumWorkers)
//   - Per-worker lock-free queues (NumWorkers × QueueSizePerWorker slots)
//   - Global fallback queue (total capacity / 4, minimum 256 slots)
//   - Metrics tracking structures
//
// NewPool validates all configuration options and returns an error if any are
// invalid. Common validation errors include:
//   - NumWorkers ≤ 0 or > 10000
//   - QueueSizePerWorker not a power of 2
//   - MaxParkTime ≤ 0 or > 1 minute
//
// The returned pool must be shut down when no longer needed to release resources:
//
//	defer pool.Shutdown(true)
//
// Parameters:
//
//	opts: Zero or more Option functions to configure the pool
//
// Returns:
//   - *Pool: A running pool ready to accept tasks
//   - error: Validation error if configuration is invalid, nil otherwise
//
// Example - Default configuration:
//
//	// Create pool with sensible defaults
//	pool, err := flock.NewPool()
//	if err != nil {
//	    log.Fatalf("Failed to create pool: %v", err)
//	}
//	defer pool.Shutdown(true)
//
//	// Pool is ready to use
//	pool.Submit(func() {
//	    fmt.Println("Hello from worker pool!")
//	})
//
// Example - CPU-bound workload:
//
//	// Configure for CPU-intensive tasks
//	pool, err := flock.NewPool(
//	    flock.WithNumWorkers(runtime.NumCPU()),
//	    flock.WithQueueSizePerWorker(256),
//	    flock.WithSpinCount(50),  // Higher spin for better latency
//	)
//	if err != nil {
//	    log.Fatalf("Failed to create pool: %v", err)
//	}
//	defer pool.Shutdown(true)
//
// Example - I/O-bound workload:
//
//	// Configure for I/O-intensive tasks (web requests, database queries)
//	pool, err := flock.NewPool(
//	    flock.WithNumWorkers(runtime.NumCPU() * 4),  // Oversubscribe for I/O
//	    flock.WithQueueSizePerWorker(1024),          // Larger queues for bursts
//	    flock.WithMaxParkTime(5 * time.Millisecond), // Wake up faster
//	)
//	if err != nil {
//	    log.Fatalf("Failed to create pool: %v", err)
//	}
//	defer pool.Shutdown(true)
//
// Example - High-throughput server:
//
//	// Configure for maximum throughput with bounded concurrency
//	pool, err := flock.NewPool(
//	    flock.WithNumWorkers(runtime.NumCPU() * 2),
//	    flock.WithQueueSizePerWorker(2048),
//	    flock.WithBlockingStrategy(flock.BlockWhenQueueFull),  // Backpressure
//	)
//	if err != nil {
//	    log.Fatalf("Failed to create pool: %v", err)
//	}
//	defer pool.Shutdown(true)
//
// Example - Custom panic handling:
//
//	// Configure with custom panic handler for production monitoring
//	pool, err := flock.NewPool(
//	    flock.WithPanicHandler(func(r interface{}) {
//	        log.Printf("Task panicked: %v", r)
//	        sentry.CaptureException(fmt.Errorf("worker panic: %v", r))
//	    }),
//	)
//	if err != nil {
//	    log.Fatalf("Failed to create pool: %v", err)
//	}
//	defer pool.Shutdown(true)
//
// Example - Worker lifecycle hooks:
//
//	// Initialize per-worker resources (database connections, buffers, etc.)
//	var dbConns sync.Map
//
//	pool, err := flock.NewPool(
//	    flock.WithWorkerHooks(
//	        func(workerID int) {
//	            // Called when worker starts
//	            conn, _ := sql.Open("postgres", dsn)
//	            dbConns.Store(workerID, conn)
//	            log.Printf("Worker %d initialized", workerID)
//	        },
//	        func(workerID int) {
//	            // Called when worker stops
//	            if conn, ok := dbConns.Load(workerID); ok {
//	                conn.(*sql.DB).Close()
//	            }
//	            log.Printf("Worker %d shutdown", workerID)
//	        },
//	    ),
//	)
//	if err != nil {
//	    log.Fatalf("Failed to create pool: %v", err)
//	}
//	defer pool.Shutdown(true)
//
// Example - Error handling on full queues:
//
//	// Configure to return errors instead of blocking
//	pool, err := flock.NewPool(
//	    flock.WithBlockingStrategy(flock.ErrorWhenQueueFull),
//	)
//	if err != nil {
//	    log.Fatalf("Failed to create pool: %v", err)
//	}
//	defer pool.Shutdown(true)
//
//	// Handle queue full errors explicitly
//	err = pool.Submit(task)
//	if err == flock.ErrQueueFull {
//	    log.Println("Queue full, implementing backoff...")
//	    time.Sleep(100 * time.Millisecond)
//	    pool.Submit(task)  // Retry
//	}
//
// Example - Validation error handling:
//
//	// Invalid configuration returns error
//	pool, err := flock.NewPool(
//	    flock.WithNumWorkers(-1),  // Invalid!
//	)
//	if err != nil {
//	    log.Printf("Invalid configuration: %v", err)
//	    // Handle error appropriately
//	}
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

// Submit submits a task to the worker pool for asynchronous execution.
//
// The task is distributed to workers using a round-robin strategy across all
// worker queues. If all queues are full, behavior depends on the configured
// BlockingStrategy:
//   - BlockWhenQueueFull: Blocks until space becomes available or pool shuts down
//   - ErrorWhenQueueFull: Returns ErrQueueFull immediately
//   - NewThreadWhenQueueFull: Executes task in a new goroutine (fallback)
//
// Submit is safe for concurrent use by multiple goroutines.
//
// Parameters:
//
//	task: A function to execute asynchronously. Must not be nil.
//
// Returns:
//   - nil on successful submission
//   - ErrNilTask if task is nil
//   - ErrPoolShutdown if pool has been shut down
//   - ErrQueueFull if queues are full and strategy is ErrorWhenQueueFull
//
// Example - Basic submission:
//
//	err := pool.Submit(func() {
//	    fmt.Println("Task executed")
//	})
//	if err != nil {
//	    log.Printf("Failed to submit: %v", err)
//	}
//
// Example - Handling queue full:
//
//	pool, _ := flock.NewPool(
//	    flock.WithBlockingStrategy(flock.ErrorWhenQueueFull),
//	)
//	err := pool.Submit(task)
//	if err == flock.ErrQueueFull {
//	    // Implement retry logic
//	    time.Sleep(100 * time.Millisecond)
//	    pool.Submit(task)
//	}
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

// Shutdown stops the worker pool and waits for workers to exit.
//
// The graceful parameter controls shutdown behavior:
//
// Graceful Shutdown (true):
//   - Stops accepting new tasks (Submit returns ErrPoolShutdown)
//   - Workers drain their queues and execute all pending tasks
//   - Blocks until all tasks complete and workers exit
//   - Waits for fallback goroutines (if using NewThreadWhenQueueFull)
//
// Immediate Shutdown (false):
//   - Stops accepting new tasks (Submit returns ErrPoolShutdown)
//   - Workers finish currently executing tasks only
//   - Remaining queued tasks are dropped (counted in stats.Dropped)
//   - Blocks until workers and fallback goroutines exit
//
// Multiple calls to Shutdown are safe. Subsequent calls after the first
// are ignored (no-op). Shutdown is safe for concurrent use, though typically
// only one goroutine should call it.
//
// After Shutdown completes, the pool cannot be restarted. Create a new pool
// if needed.
//
// Parameters:
//
//	graceful: true to drain queues, false to stop immediately
//
// Example - Graceful shutdown:
//
//	pool, _ := flock.NewPool()
//	defer pool.Shutdown(true)  // Ensures all tasks complete
//
//	for i := 0; i < 1000; i++ {
//	    pool.Submit(task)
//	}
//	// Shutdown waits for all 1000 tasks to complete
//
// Example - Immediate shutdown:
//
//	pool, _ := flock.NewPool()
//
//	for i := 0; i < 1000; i++ {
//	    pool.Submit(task)
//	}
//
//	pool.Shutdown(false)  // May drop queued tasks
//	stats := pool.Stats()
//	fmt.Printf("Dropped %d tasks\n", stats.Dropped)
//
// Example - Shutdown with timeout:
//
//	done := make(chan struct{})
//	go func() {
//	    pool.Shutdown(true)
//	    close(done)
//	}()
//
//	select {
//	case <-done:
//	    log.Println("Graceful shutdown completed")
//	case <-time.After(30 * time.Second):
//	    log.Println("Timeout, forcing immediate shutdown")
//	    pool.Shutdown(false)
//	}
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

// Wait blocks until all submitted tasks have completed execution.
//
// Wait does NOT shut down the pool - after Wait returns, the pool remains
// active and can accept new tasks. This is useful for checkpointing or
// ensuring all tasks complete before proceeding to the next phase.
//
// Tasks that panic are still counted as completed. Tasks rejected with
// ErrQueueFull or ErrPoolShutdown are not tracked by Wait.
//
// Wait is safe for concurrent use by multiple goroutines, though typically
// only one goroutine should call Wait.
//
// Example - Wait for batch completion:
//
//	// Submit batch of tasks
//	for i := 0; i < 1000; i++ {
//	    pool.Submit(processTask(i))
//	}
//
//	// Wait for batch to complete
//	pool.Wait()
//	fmt.Println("Batch complete, starting next batch")
//
//	// Submit more tasks
//	for i := 1000; i < 2000; i++ {
//	    pool.Submit(processTask(i))
//	}
//
// Example - Periodic checkpoints:
//
//	for batchNum := 0; batchNum < 10; batchNum++ {
//	    for i := 0; i < 100; i++ {
//	        pool.Submit(task)
//	    }
//	    pool.Wait()
//	    log.Printf("Completed batch %d", batchNum)
//	}
func (p *Pool) Wait() {
	p.submitWg.Wait()
}

// Stats returns a snapshot of current pool statistics and performance metrics.
//
// The returned Stats struct contains comprehensive information about:
//   - Task counts (submitted, completed, dropped, rejected, in-flight)
//   - Queue utilization and capacity
//   - Execution latency (average and maximum)
//   - Per-worker statistics (tasks executed, failures, state)
//
// Stats are collected without locks for performance, so values may be slightly
// inconsistent during concurrent operations. For example, Submitted might be
// read slightly before Completed, causing temporary inaccuracies. These
// inconsistencies are negligible for monitoring purposes.
//
// Stats can be called at any time, including during active task execution or
// after shutdown. It is safe for concurrent use by multiple goroutines.
//
// The Stats struct is a snapshot - modifications to the returned struct do not
// affect the pool's internal state.
//
// Returns:
//
//	A Stats struct containing current pool metrics
//
// Example - Basic monitoring:
//
//	stats := pool.Stats()
//	fmt.Printf("Throughput: %d/%d completed\n", stats.Completed, stats.Submitted)
//	fmt.Printf("Queue utilization: %.2f%%\n", stats.Utilization)
//	fmt.Printf("Average latency: %v\n", stats.LatencyAvg)
//
// Example - Health check:
//
//	stats := pool.Stats()
//	if stats.Utilization > 90.0 {
//	    log.Println("WARNING: Queue utilization high, consider scaling")
//	}
//	if stats.FallbackExecuted > stats.Completed*0.1 {
//	    log.Println("WARNING: High fallback rate, increase queue size")
//	}
//
// Example - Per-worker analysis:
//
//	stats := pool.Stats()
//	for _, ws := range stats.WorkerStats {
//	    failureRate := float64(ws.TasksFailed) / float64(ws.TasksExecuted) * 100
//	    if failureRate > 5.0 {
//	        log.Printf("Worker %d has high failure rate: %.2f%%",
//	            ws.WorkerID, failureRate)
//	    }
//	}
//
// Example - Periodic metrics export:
//
//	ticker := time.NewTicker(10 * time.Second)
//	go func() {
//	    for range ticker.C {
//	        stats := pool.Stats()
//
//	        // Export to monitoring system
//	        metrics.Gauge("pool.submitted").Set(stats.Submitted)
//	        metrics.Gauge("pool.completed").Set(stats.Completed)
//	        metrics.Gauge("pool.inflight").Set(stats.InFlight)
//	        metrics.Gauge("pool.utilization").Set(stats.Utilization)
//	        metrics.Histogram("pool.latency").Observe(stats.LatencyAvg.Seconds())
//	    }
//	}()
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

// IsShutdown returns true if the pool has been shut down and is no longer
// accepting new tasks.
//
// A pool is considered shutdown when Shutdown() has been called and the
// shutdown process has completed. During graceful shutdown (while tasks are
// draining), IsShutdown returns false until all tasks complete.
//
// IsShutdown is useful for:
//   - Checking if a pool is still usable before submitting tasks
//   - Coordinating shutdown across multiple components
//   - Testing and debugging shutdown behavior
//
// IsShutdown is safe for concurrent use by multiple goroutines.
//
// Returns:
//
//	true if pool is fully shutdown, false otherwise
//
// Example - Conditional submission:
//
//	if !pool.IsShutdown() {
//	    err := pool.Submit(task)
//	    if err != nil {
//	        log.Printf("Submission failed: %v", err)
//	    }
//	} else {
//	    log.Println("Pool is shutdown, cannot submit task")
//	}
//
// Example - Graceful degradation:
//
//	var primaryPool, backupPool *flock.Pool
//
//	func submitTask(task func()) error {
//	    if !primaryPool.IsShutdown() {
//	        return primaryPool.Submit(task)
//	    }
//	    if !backupPool.IsShutdown() {
//	        return backupPool.Submit(task)
//	    }
//	    return errors.New("all pools are shutdown")
//	}
//
// Example - Shutdown coordination:
//
//	// Coordinated shutdown of multiple pools
//	pools := []*flock.Pool{pool1, pool2, pool3}
//
//	// Initiate shutdown
//	for _, p := range pools {
//	    go p.Shutdown(true)
//	}
//
//	// Wait for all to complete
//	for {
//	    allShutdown := true
//	    for _, p := range pools {
//	        if !p.IsShutdown() {
//	            allShutdown = false
//	            break
//	        }
//	    }
//	    if allShutdown {
//	        break
//	    }
//	    time.Sleep(100 * time.Millisecond)
//	}
func (p *Pool) IsShutdown() bool {
	return p.state.Load().(PoolState) == poolStateStopped
}

// NumWorkers returns the number of worker goroutines in the pool.
//
// This value is fixed at pool creation and never changes during the pool's
// lifetime. It represents the maximum number of tasks that can execute
// concurrently.
//
// NumWorkers is useful for:
//   - Calculating total queue capacity (NumWorkers × QueueSizePerWorker)
//   - Understanding concurrency limits
//   - Debugging and logging pool configuration
//
// NumWorkers is safe for concurrent use by multiple goroutines.
//
// Returns:
//
//	The number of workers, as specified during pool creation
//
// Example - Capacity calculation:
//
//	totalCapacity := pool.NumWorkers() * queueSizePerWorker
//	fmt.Printf("Pool can queue up to %d tasks\n", totalCapacity)
//
// Example - Dynamic sizing:
//
//	numWorkers := pool.NumWorkers()
//	optimalBatchSize := numWorkers * 10
//	fmt.Printf("Recommended batch size: %d\n", optimalBatchSize)
//
// Example - Logging configuration:
//
//	log.Printf("Pool initialized with %d workers", pool.NumWorkers())
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
