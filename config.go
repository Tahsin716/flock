package flock

import (
	"runtime"
	"time"
)

// Config contains all configuration options for the worker pool
type Config struct {
	// NumWorkers is the number of worker goroutines
	// Default value runtime.NumCPU()
	NumWorkers int

	// QueueSizePerWorker is the initial size of each worker's deque (Must be a power of 2)
	// The deque grows dynamically up to maxDequeCapacity (65536)
	// Defaults value 256
	QueueSizePerWorker int

	// PanicHandler is called when a task panics
	// If nil, panics are silently caught (stack trace captured internally)
	PanicHandler func(interface{})

	// OnWorkerStart is called when a worker starts
	// Useful for initialization, logging, or tracing
	OnWorkerStart func(workerID int)

	// OnWorkerStop is called when a worker stops
	// Useful for cleanup, logging, or tracing
	OnWorkerStop func(workerID int)

	// PinWorkerThreads attempts to pin workers to OS threads
	// Can improve cache locality but reduces scheduling flexibility
	// Default: false (let Go scheduler manage)
	PinWorkerThreads bool

	// MaxParkTime is the maximum time a worker will sleep when idle
	// Lower values: better latency, higher CPU usage when idle
	// Higher values: worse latency, lower CPU usage when idle
	// Default: 10ms (good balance)
	MaxParkTime time.Duration

	// SpinCount is the number of iterations to spin before parking
	// Higher values: better latency for bursty workloads, higher CPU usage
	// Lower values: worse latency, lower CPU usage
	// Default: 30 iterations (~1-10µs on modern CPUs)
	SpinCount int

	BlockingStrategy BlockingStrategy
}

// BlockingStrategy determines how the pool behaves when all worker queues are full.
//
// The strategy affects submission latency, memory usage, and backpressure handling.
// Choose based on your application's requirements:
//
//   - BlockWhenQueueFull: Best for backpressure control and bounded memory
//   - ErrorWhenQueueFull: Best for explicit queue full handling
//   - NewThreadWhenQueueFull: Best for bursty workloads with occasional spillover
type BlockingStrategy int32

const (
	// BlockWhenQueueFull causes Submit to block when all worker queues are full.
	// The call will wait until space becomes available or the pool shuts down.
	//
	// Use when:
	//   - You want automatic backpressure control
	//   - Memory usage must be bounded
	//   - Occasional blocking is acceptable
	//
	// Behavior:
	//   - Submit blocks in a spin loop checking for available space
	//   - Returns nil when task is queued
	//   - Returns ErrPoolShutdown if pool shuts down while blocking
	//
	// Example:
	//  pool, _ := flock.NewPool(
	//      flock.WithBlockingStrategy(flock.BlockWhenQueueFull),
	//  )
	//  pool.Submit(task)  // Blocks if queues are full
	BlockWhenQueueFull BlockingStrategy = iota

	// ErrorWhenQueueFull causes Submit to return ErrQueueFull immediately
	// when all worker queues are full, without blocking.
	//
	// Use when:
	//   - You need explicit control over queue full handling
	//   - You want to implement custom retry or drop logic
	//   - Non-blocking submission is critical
	//
	// Behavior:
	//   - Submit returns immediately if queues are full
	//   - Returns ErrQueueFull (check with errors.Is)
	//   - Task is NOT executed and NOT counted as dropped
	//   - Increments stats.Rejected counter
	//
	// Example:
	//  pool, _ := flock.NewPool(
	//      flock.WithBlockingStrategy(flock.ErrorWhenQueueFull),
	//  )
	//  err := pool.Submit(task)
	//  if err == flock.ErrQueueFull {
	//      // Implement retry, drop, or alternative handling
	//      log.Println("Queue full, will retry later")
	//  }
	ErrorWhenQueueFull

	// NewThreadWhenQueueFull causes Submit to spawn a new goroutine to execute
	// the task when all worker queues are full.
	//
	// Use when:
	//   - Workload is bursty with occasional spikes
	//   - Task execution is more important than resource bounds
	//   - Occasional goroutine creation overhead is acceptable
	//
	// Behavior:
	//   - Submit spawns a new goroutine if queues are full
	//   - Returns nil immediately (non-blocking)
	//   - Task executes in the fallback goroutine
	//   - Increments stats.FallbackExecuted counter
	//
	// Performance Note:
	//   High fallback rates indicate undersized queues. Monitor
	//   stats.FallbackExecuted and increase queue size if needed.
	//
	// Example:
	//  pool, _ := flock.NewPool(
	//      flock.WithBlockingStrategy(flock.NewThreadWhenQueueFull),
	//  )
	//  pool.Submit(task)  // Never blocks, spawns goroutine if needed
	NewThreadWhenQueueFull
)

// defaultConfig returns a Config with production-ready defaults
func defaultConfig() Config {
	return Config{
		NumWorkers:         runtime.NumCPU(), // runtime.NumCPU()
		QueueSizePerWorker: 256,
		PanicHandler:       nil,
		MaxParkTime:        10 * time.Millisecond,
		SpinCount:          30,
		PinWorkerThreads:   false,
		BlockingStrategy:   BlockWhenQueueFull,
	}
}

// validate checks the configuration and returns an error if invalid
func (c *Config) validate() error {
	if c.NumWorkers <= 0 {
		return errInvalidConfig("NumWorkers must be positive")
	}

	if c.NumWorkers > 10000 {
		return errInvalidConfig("NumWorkers too large (>10000), likely a mistake")
	}

	if c.QueueSizePerWorker <= 0 {
		return errInvalidConfig("QueueSizePerWorker must be positive")
	}

	if c.QueueSizePerWorker&(c.QueueSizePerWorker-1) != 0 {
		return errInvalidConfig("QueueSizePerWorker must be a power of 2")
	}

	if c.QueueSizePerWorker > 100000 {
		return errInvalidConfig("QueueSizePerWorker too large (>100000), use dynamic sizing")
	}

	if c.MaxParkTime <= 0 {
		return errInvalidConfig("MaxParkTime must be positive")
	}

	if c.MaxParkTime > 1*time.Minute {
		return errInvalidConfig("MaxParkTime too large (>1min), workers may appear stuck")
	}

	if c.SpinCount < 0 {
		return errInvalidConfig("SpinCount must be >= 0")
	}

	if c.SpinCount > 10000 {
		return errInvalidConfig("SpinCount too large (>10000), will waste CPU")
	}

	if c.BlockingStrategy < 0 || c.BlockingStrategy > 2 {
		return errInvalidConfig("BlockingStrategy provided is invalid")
	}

	return nil
}

//
// ---- Option Pattern ----
//

// Option is a function that modifies a Config
type Option func(*Config)

// WithNumWorkers sets the number of worker goroutines in the pool.
//
// Workers execute tasks concurrently. Each worker has its own queue and
// processes tasks independently. The number of workers determines maximum
// concurrency.
//
// Guidelines:
//   - CPU-bound tasks: runtime.NumCPU() (default)
//   - I/O-bound tasks: 2-4× runtime.NumCPU()
//   - Database queries: Based on connection pool size
//   - Network requests: Higher values (10-100+)
//
// Constraints:
//   - Must be > 0
//   - Must be ≤ 10000 (sanity check)
//
// Default: runtime.NumCPU()
//
// Example - CPU-bound workload:
//
//	pool, _ := flock.NewPool(
//	    flock.WithNumWorkers(runtime.NumCPU()),
//	)
//
// Example - I/O-bound workload:
//
//	pool, _ := flock.NewPool(
//	    flock.WithNumWorkers(runtime.NumCPU() * 4),
//	)
func WithNumWorkers(n int) Option {
	return func(c *Config) { c.NumWorkers = n }
}

// WithQueueSizePerWorker sets the capacity of each worker's task queue.
//
// Each worker has an independent lock-free queue. Larger queues can absorb
// bursts but use more memory. Queue size MUST be a power of 2 for efficient
// bitwise operations.
//
// Guidelines:
//   - Low latency workloads: 128-512
//   - High throughput workloads: 1024-4096
//   - Bursty workloads: 2048-8192
//
// Constraints:
//   - Must be > 0
//   - Must be a power of 2 (128, 256, 512, 1024, etc.)
//   - Must be ≤ 100000 (memory sanity check)
//
// Total queue capacity: NumWorkers × QueueSizePerWorker
//
// Default: 256
//
// Example - Small queues for low latency:
//
//	pool, _ := flock.NewPool(
//	    flock.WithQueueSizePerWorker(256),
//	)
//
// Example - Large queues for burst absorption:
//
//	pool, _ := flock.NewPool(
//	    flock.WithQueueSizePerWorker(4096),
//	)
func WithQueueSizePerWorker(size int) Option {
	return func(c *Config) { c.QueueSizePerWorker = size }
}

// WithPanicHandler sets a custom panic handler for task panics.
//
// When a task panics, the handler is called with the panic value. The pool
// continues operating normally - panics do not crash workers or the pool.
//
// If no handler is set, panics are caught silently with stack traces captured
// and logged to stderr.
//
// The handler is called synchronously in the worker goroutine where the panic
// occurred. Keep handler logic fast to avoid blocking workers.
//
// Handler Parameter:
//
//	r interface{}: The value passed to panic() (any type)
//
// Default: nil (silent capture with stderr logging)
//
// Example - Logging panics:
//
//	pool, _ := flock.NewPool(
//	    flock.WithPanicHandler(func(r interface{}) {
//	        log.Printf("Task panicked: %v", r)
//	    }),
//	)
//
// Example - Error tracking service:
//
//	pool, _ := flock.NewPool(
//	    flock.WithPanicHandler(func(r interface{}) {
//	        sentry.CaptureException(fmt.Errorf("task panic: %v", r))
//	    }),
//	)
//
// Example - Custom recovery:
//
//	pool, _ := flock.NewPool(
//	    flock.WithPanicHandler(func(r interface{}) {
//	        switch err := r.(type) {
//	        case error:
//	            log.Printf("Task error: %v", err)
//	        case string:
//	            log.Printf("Task panic: %s", err)
//	        default:
//	            log.Printf("Task panic: %v", r)
//	        }
//	    }),
//	)
func WithPanicHandler(handler func(interface{})) Option {
	return func(c *Config) { c.PanicHandler = handler }
}

// WithWorkerHooks sets lifecycle hooks for worker start and stop events.
//
// Hooks are called synchronously when workers start and stop. Use for:
//   - Initializing per-worker resources (database connections, buffers)
//   - Cleaning up per-worker resources
//   - Logging and monitoring
//   - Distributed tracing setup
//
// OnWorkerStart is called in the worker goroutine after it starts but before
// processing any tasks. OnWorkerStop is called in the worker goroutine after
// it stops processing tasks but before the goroutine exits.
//
// Keep hook logic fast to avoid delaying worker startup/shutdown.
//
// Parameters:
//
//	onStart: Called when worker starts (can be nil)
//	onStop: Called when worker stops (can be nil)
//
// Hook Parameter:
//
//	workerID int: Unique worker identifier (0 to NumWorkers-1)
//
// Default: nil (no hooks)
//
// Example - Resource initialization:
//
//	var dbConns sync.Map
//
//	pool, _ := flock.NewPool(
//	    flock.WithWorkerHooks(
//	        func(workerID int) {
//	            conn, _ := sql.Open("postgres", dsn)
//	            dbConns.Store(workerID, conn)
//	        },
//	        func(workerID int) {
//	            if conn, ok := dbConns.Load(workerID); ok {
//	                conn.(*sql.DB).Close()
//	            }
//	        },
//	    ),
//	)
//
// Example - Logging and monitoring:
//
//	pool, _ := flock.NewPool(
//	    flock.WithWorkerHooks(
//	        func(workerID int) {
//	            log.Printf("Worker %d started", workerID)
//	            metrics.Gauge("workers.active").Inc()
//	        },
//	        func(workerID int) {
//	            log.Printf("Worker %d stopped", workerID)
//	            metrics.Gauge("workers.active").Dec()
//	        },
//	    ),
//	)
//
// Example - Distributed tracing:
//
//	pool, _ := flock.NewPool(
//	    flock.WithWorkerHooks(
//	        func(workerID int) {
//	            span := tracer.StartSpan(fmt.Sprintf("worker-%d", workerID))
//	            defer span.Finish()
//	        },
//	        nil,  // No stop hook needed
//	    ),
//	)
func WithWorkerHooks(onStart, onStop func(int)) Option {
	return func(c *Config) {
		c.OnWorkerStart = onStart
		c.OnWorkerStop = onStop
	}
}

// WithPinWorkerThreads controls whether workers are pinned to OS threads.
//
// When enabled, each worker goroutine is locked to a dedicated OS thread using
// runtime.LockOSThread. This can improve cache locality and reduce context
// switching for CPU-bound workloads.
//
// Trade-offs:
//
//	Pros:
//	  - Better CPU cache locality
//	  - Reduced context switching
//	  - More predictable performance for CPU-bound tasks
//
//	Cons:
//	  - Reduces Go scheduler flexibility
//	  - Uses more OS resources (one thread per worker)
//	  - May hurt performance for I/O-bound tasks
//	  - Can cause issues with thread-local storage
//
// Recommendation:
//
//	Only enable if profiling shows significant cache benefits. Most workloads
//	perform better with the default (false) to leverage the scheduler in Go.
//
// Default: false
//
// Example - Enable for CPU-intensive workload:
//
//	pool, _ := flock.NewPool(
//	    flock.WithPinWorkerThreads(true),
//	    flock.WithNumWorkers(runtime.NumCPU()),
//	)
//
// Example - Disable for I/O workload (default):
//
//	pool, _ := flock.NewPool(
//	    flock.WithPinWorkerThreads(false),
//	)
func WithPinWorkerThreads(enabled bool) Option {
	return func(c *Config) { c.PinWorkerThreads = enabled }
}

// WithMaxParkTime sets the maximum duration a worker sleeps when idle.
//
// When a worker has no tasks, it enters a parking state. MaxParkTime controls
// how long the worker sleeps before waking to check for new tasks.
//
// Trade-offs:
//
//	Lower values (1-5)ms:
//	  - Better latency for incoming tasks
//	  - Higher CPU usage when idle (more frequent wake-ups)
//	  - Better for latency-sensitive applications
//
//	Higher values (20-100)ms:
//	  - Lower CPU usage when idle
//	  - Higher latency for incoming tasks
//	  - Better for power efficiency
//
// Workers are still woken immediately via signal when tasks arrive, so this
// primarily affects recovery from idle states.
//
// Constraints:
//   - Must be > 0
//   - Must be ≤ 1 minute (sanity check)
//
// Default: 10ms (balanced latency and CPU usage)
//
// Example - Low latency configuration:
//
//	pool, _ := flock.NewPool(
//	    flock.WithMaxParkTime(1 * time.Millisecond),
//	)
//
// Example - Low CPU usage configuration:
//
//	pool, _ := flock.NewPool(
//	    flock.WithMaxParkTime(50 * time.Millisecond),
//	)
func WithMaxParkTime(d time.Duration) Option {
	return func(c *Config) { c.MaxParkTime = d }
}

// WithSpinCount sets the number of iterations to spin before parking.
//
// When a worker finds no tasks, it actively spins (busy-waits) checking for
// new work before entering a parked (sleeping) state. Spinning reduces latency
// at the cost of CPU usage.
//
// Trade-offs:
//
//	Higher values (50-200):
//	  - Lower latency for bursty workloads
//	  - Higher CPU usage (more time spinning)
//	  - Better when tasks arrive in quick succession
//
//	Lower values (5-20):
//	  - Higher latency for incoming tasks
//	  - Lower CPU usage (less time spinning)
//	  - Better for steady-state workloads
//
// Each spin iteration includes a runtime.Gosched() to yield the CPU,
// preventing total CPU lockup. Typical spin duration: 1-10µs on modern CPUs.
//
// Constraints:
//   - Must be ≥ 0 (0 = no spinning, immediate park)
//   - Must be ≤ 10000 (sanity check to prevent CPU waste)
//
// Default: 30 (balanced for most workloads)
//
// Example - High-frequency, low-latency workload:
//
//	pool, _ := flock.NewPool(
//	    flock.WithSpinCount(100),
//	)
//
// Example - Batch processing, optimize CPU usage:
//
//	pool, _ := flock.NewPool(
//	    flock.WithSpinCount(10),
//	)
//
// Example - Disable spinning entirely:
//
//	pool, _ := flock.NewPool(
//	    flock.WithSpinCount(0),
//	)
func WithSpinCount(n int) Option {
	return func(c *Config) { c.SpinCount = n }
}

// WithBlockingStrategy sets the strategy for handling full worker queues.
//
// See BlockingStrategy documentation for detailed behavior of each strategy.
//
// Default: BlockWhenQueueFull
//
// Example - Block on full (default):
//
//	pool, _ := flock.NewPool(
//	    flock.WithBlockingStrategy(flock.BlockWhenQueueFull),
//	)
//
// Example - Return error on full:
//
//	pool, _ := flock.NewPool(
//	    flock.WithBlockingStrategy(flock.ErrorWhenQueueFull),
//	)
//
// Example - Spawn goroutine on full:
//
//	pool, _ := flock.NewPool(
//	    flock.WithBlockingStrategy(flock.NewThreadWhenQueueFull),
//	)
func WithBlockingStrategy(b BlockingStrategy) Option {
	return func(c *Config) { c.BlockingStrategy = b }
}
