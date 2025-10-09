package flock

import "time"

// Stats contains pool statistics
type Stats struct {
	// Submitted is the total number of tasks submitted to the pool.
	Submitted uint64
	// Completed is the total number of tasks that have finished execution.
	Completed uint64
	// Rejected is the total number of tasks rejected by TrySubmit because queues were full.
	Rejected uint64
	// FallbackExecuted is the total number of tasks executed in the caller's goroutine.
	FallbackExecuted uint64
	// InFlight is the estimated number of tasks currently running or in queue.
	InFlight uint64
	// Utilization is the percentage of total queue capacity currently in use.
	Utilization float64
	// WorkerStats contains statistics for each individual worker.
	WorkerStats []WorkerStats
	// NumWorkers is the total number of workers in the pool.
	NumWorkers int
	// TotalQueueDepth is the combined number of tasks in all worker queues.
	TotalQueueDepth int
	// TotalQueueCapacity is the combined capacity of all worker queues.
	TotalQueueCapacity int
	// LatencyAvg is the average execution time for completed tasks.
	LatencyAvg time.Duration
	// LatencyMax is the maximum execution time observed for any single task.
	LatencyMax time.Duration
}

// WorkerStats contains per-worker statistics
type WorkerStats struct {
	// WorkerID is the unique identifier for the worker.
	WorkerID int
	// TasksExecuted is the total number of tasks executed by this worker.
	TasksExecuted uint64
	// TasksFailed is the total number of tasks that panicked during execution by this worker.
	TasksFailed uint64
	// Capacity is the queue capacity for this specific worker.
	Capacity int
	// State is the current state of the worker (e.g., Running, Parked).
	State WorkerState
}
