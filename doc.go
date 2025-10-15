// Package flock provides a high-performance, lock-free worker pool implementation for Go.
//
// Flock is designed for scenarios requiring efficient concurrent task execution with
// minimal overhead. It uses lock-free MPSC (Multi-Producer Single-Consumer) queues
// and provides fine-grained control over blocking behavior, worker management, and
// performance tuning.
//
// # Key Features
//
//   - Lock-free MPSC queues for minimal contention
//   - Configurable blocking strategies (block, error, or spawn new goroutines)
//   - Adaptive parking with spinning for low-latency task dispatch
//   - Comprehensive metrics and statistics
//   - Panic recovery with customizable handlers
//   - Worker lifecycle hooks for monitoring and tracing
//   - Graceful and immediate shutdown modes
//
// # Quick Start
//
// Basic usage with default configuration:
//
//	pool, err := flock.NewPool()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer pool.Shutdown(true)
//
//	// Submit tasks
//	for i := 0; i < 100; i++ {
//	    i := i
//	    err := pool.Submit(func() {
//	        fmt.Printf("Task %d executed\n", i)
//	    })
//	    if err != nil {
//	        log.Printf("Failed to submit task: %v", err)
//	    }
//	}
//
//	// Wait for all tasks to complete
//	pool.Wait()
//
// # Configuration
//
// Customize the pool using functional options:
//
//	pool, err := flock.NewPool(
//	    flock.WithNumWorkers(8),
//	    flock.WithQueueSizePerWorker(512),
//	    flock.WithBlockingStrategy(flock.ErrorWhenQueueFull),
//	    flock.WithMaxParkTime(5 * time.Millisecond),
//	    flock.WithSpinCount(100),
//	)
//
// # Blocking Strategies
//
// Flock supports three blocking strategies when all worker queues are full:
//
// BlockWhenQueueFull (default): Blocks the Submit call until space becomes available
// or the pool shuts down. Use for backpressure control.
//
//	pool, _ := flock.NewPool(
//	    flock.WithBlockingStrategy(flock.BlockWhenQueueFull),
//	)
//
// ErrorWhenQueueFull: Returns ErrQueueFull immediately when queues are full.
// Use when you need to handle queue full conditions explicitly.
//
//	pool, _ := flock.NewPool(
//	    flock.WithBlockingStrategy(flock.ErrorWhenQueueFull),
//	)
//
//	err := pool.Submit(task)
//	if err == flock.ErrQueueFull {
//	    // Handle queue full scenario
//	}
//
// NewThreadWhenQueueFull: Spawns a new goroutine to execute the task when
// queues are full. Use for bursty workloads where occasional spillover is acceptable.
//
//	pool, _ := flock.NewPool(
//	    flock.WithBlockingStrategy(flock.NewThreadWhenQueueFull),
//	)
//
// # Performance Tuning
//
// Workers use adaptive parking to balance latency and CPU usage:
//
//   - SpinCount: Number of iterations to actively spin before parking (default: 30)
//   - MaxParkTime: Maximum duration to park when idle (default: 10ms)
//
// Lower latency (higher CPU usage):
//
//	pool, _ := flock.NewPool(
//	    flock.WithSpinCount(100),        // Spin longer
//	    flock.WithMaxParkTime(1 * time.Millisecond),  // Wake more frequently
//	)
//
// Lower CPU usage (higher latency):
//
//	pool, _ := flock.NewPool(
//	    flock.WithSpinCount(10),         // Spin briefly
//	    flock.WithMaxParkTime(50 * time.Millisecond), // Park longer
//	)
//
// # Error Handling
//
// Tasks can panic without crashing the worker pool. Use a custom panic handler
// for logging or recovery:
//
//	pool, _ := flock.NewPool(
//	    flock.WithPanicHandler(func(r interface{}) {
//	        log.Printf("Task panicked: %v", r)
//	        // Custom recovery logic
//	    }),
//	)
//
// # Monitoring and Observability
//
// Use worker lifecycle hooks for monitoring:
//
//	pool, _ := flock.NewPool(
//	    flock.WithWorkerHooks(
//	        func(workerID int) {
//	            log.Printf("Worker %d started", workerID)
//	        },
//	        func(workerID int) {
//	            log.Printf("Worker %d stopped", workerID)
//	        },
//	    ),
//	)
//
// Access detailed statistics:
//
//	stats := pool.Stats()
//	fmt.Printf("Submitted: %d, Completed: %d, InFlight: %d\n",
//	    stats.Submitted, stats.Completed, stats.InFlight)
//	fmt.Printf("Queue Utilization: %.2f%%\n", stats.Utilization)
//	fmt.Printf("Avg Latency: %v, Max Latency: %v\n",
//	    stats.LatencyAvg, stats.LatencyMax)
//
//	for _, ws := range stats.WorkerStats {
//	    fmt.Printf("Worker %d: %d tasks, %d failures, state: %s\n",
//	        ws.WorkerID, ws.TasksExecuted, ws.TasksFailed, ws.State)
//	}
//
// # Shutdown
//
// Graceful shutdown waits for all queued tasks to complete:
//
//	pool.Shutdown(true)
//
// Immediate shutdown stops after currently executing tasks finish:
//
//	pool.Shutdown(false)
//
// Check if pool is shutdown:
//
//	if pool.IsShutdown() {
//	    log.Println("Pool is no longer accepting tasks")
//	}
//
// # Advanced Usage
//
// Pin workers to OS threads for improved cache locality:
//
//	pool, _ := flock.NewPool(
//	    flock.WithPinWorkerThreads(true),
//	)
//
// Note: This reduces Go scheduler flexibility and should only be used
// when profiling shows significant cache benefits.
//
// # Best Practices
//
//   - Choose NumWorkers based on your workload (CPU-bound: runtime.NumCPU(),
//     I/O-bound: higher values)
//   - Set QueueSizePerWorker to accommodate bursts (must be power of 2)
//   - Use BlockWhenQueueFull for backpressure control
//   - Use ErrorWhenQueueFull when you need explicit queue full handling
//   - Use NewThreadWhenQueueFull for bursty workloads with occasional spillover
//   - Always call Shutdown(true) or defer pool.Shutdown(true) to ensure cleanup
//   - Monitor stats.Utilization to tune queue sizes
//   - Adjust SpinCount and MaxParkTime based on latency requirements
//
// # Thread Safety
//
// All exported methods are safe for concurrent use. Multiple goroutines can
// submit tasks simultaneously without external synchronization.
//
// # Performance Characteristics
//
//   - Task submission: O(1) lock-free operation
//   - Task execution: O(1) per task
//   - Memory: O(NumWorkers * QueueSizePerWorker)
//   - Latency: Microseconds to milliseconds (depends on SpinCount/MaxParkTime)
//
// # Comparison with sync.Pool
//
// Flock is not a replacement for sync.Pool. Use sync.Pool for object reuse
// and Flock for task execution with worker pools.
//
// # License
//
// See the LICENSE file in the repository root for license information.
package flock
