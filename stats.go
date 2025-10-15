package flock

import "time"

// Stats contains comprehensive statistics about pool operation and performance.
// All counters are snapshots taken at the time Stats() is called and may be
// slightly inconsistent during concurrent operations due to lock-free reads.
//
// Example:
//
//	stats := pool.Stats()
//	fmt.Printf("Success rate: %.2f%%\n",
//	    float64(stats.Completed) / float64(stats.Submitted) * 100)
type Stats struct {
	// Submitted is the total number of tasks submitted to the pool since creation.
	// This includes tasks that were successfully queued, rejected, or dropped.
	Submitted uint64

	// Completed is the total number of tasks that have finished execution.
	// This includes both successful executions and tasks that panicked.
	Completed uint64

	// Dropped is the total number of tasks dropped during non-graceful shutdown.
	// Tasks in worker queues when Shutdown(false) is called are counted as dropped.
	Dropped uint64

	// Rejected is the total number of tasks rejected due to full queues when
	// using the ErrorWhenQueueFull blocking strategy. Tasks rejected during
	// shutdown are not counted here.
	Rejected uint64

	// FallbackExecuted is the total number of tasks executed in fallback goroutines
	// when using the NewThreadWhenQueueFull strategy. High values may indicate
	// undersized worker queues.
	FallbackExecuted uint64

	// InFlight is the estimated number of tasks currently queued or executing.
	// Calculated as: Submitted - Completed - Dropped - Rejected
	// This value may be temporarily inaccurate during concurrent operations.
	InFlight uint64

	// Utilization is the percentage of total queue capacity currently in use.
	// Range: 0.0 to 100.0. High values (>80%) suggest queues may be undersized.
	// Calculated as: (TotalQueueDepth / TotalQueueCapacity) * 100
	Utilization float64

	// WorkerStats contains detailed statistics for each individual worker.
	// The slice length equals NumWorkers, with one entry per worker.
	WorkerStats []WorkerStats

	// NumWorkers is the total number of workers in the pool.
	// This value is fixed at pool creation and does not change.
	NumWorkers int

	// TotalQueueDepth is the combined number of tasks currently in all worker queues.
	// Does not include tasks currently executing, only queued tasks.
	TotalQueueDepth int

	// TotalQueueCapacity is the combined capacity of all worker queues.
	// Calculated as: NumWorkers * QueueSizePerWorker
	TotalQueueCapacity int

	// LatencyAvg is the average execution time for all completed tasks.
	// This measures actual task execution time, not queuing time.
	// Zero if no tasks have completed.
	LatencyAvg time.Duration

	// LatencyMax is the maximum execution time observed for any single task.
	// Useful for identifying outliers or slow tasks.
	// Zero if no tasks have completed.
	LatencyMax time.Duration
}

// WorkerStats contains statistics for an individual worker goroutine.
// Each worker maintains its own counters to avoid contention.
//
// Example:
//
//	stats := pool.Stats()
//	for _, ws := range stats.WorkerStats {
//	    if ws.TasksFailed > 0 {
//	        log.Printf("Worker %d has %d failed tasks", ws.WorkerID, ws.TasksFailed)
//	    }
//	}
type WorkerStats struct {
	// WorkerID is the unique identifier for this worker (0-indexed).
	// Corresponds to the worker's position in the pool's worker slice.
	WorkerID int

	// TasksExecuted is the total number of tasks this worker has executed.
	// Includes both successful executions and tasks that panicked.
	TasksExecuted uint64

	// TasksFailed is the total number of tasks that panicked during execution.
	// These tasks are still counted in TasksExecuted.
	// High values may indicate problematic tasks or input data.
	TasksFailed uint64

	// Capacity is the maximum number of tasks this worker's queue can hold.
	// This value is fixed at pool creation (QueueSizePerWorker).
	Capacity int

	// State is the current operational state of the worker.
	// Possible values:
	//   - "RUNNING": Actively executing a task
	//   - "SPINNING": Actively checking for work (hot loop)
	//   - "PARKED": Sleeping, waiting for work or timeout
	//   - "SHUTDOWN": Worker has stopped (pool is shutdown)
	State string
}
