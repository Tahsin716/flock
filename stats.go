package flock

import "time"

// Stats contains pool statistics
type Stats struct {
	Submitted          uint64
	Completed          uint64
	Rejected           uint64
	Stolen             uint64
	FallbackExecuted   uint64
	InFlight           uint64
	Utilization        float64
	WorkerStats        []WorkerStats
	NumWorkers         int
	TotalQueueDepth    int
	TotalQueueCapacity int
	LatencyAvg         time.Duration
	LatencyMax         time.Duration
}

// WorkerStats contains per-worker statistics
type WorkerStats struct {
	WorkerID         int
	TasksExecuted    uint64
	TasksStolen      uint64
	ExternalDepth    int
	LocalDepth       int
	ExternalCapacity int
	LocalCapacity    int
	State            WorkerState
}
