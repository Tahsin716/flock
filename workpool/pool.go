package workpool

import (
	"context"
	"sync"
	"time"
)

// Job represents a unit of work
type Job[T any] func(context.Context) (T, error)

// Result holds the outcome of a job
type Result[T any] struct {
	Value T
	Error error
}

// worker represents a reusable goroutine
type worker[T any] struct {
	jobCh chan Job[T]
	pool  *Pool[T]

	// Last activity for cleanup
	lastUsed int64 // atomic timestamp
}

// Pool is a high-performance worker pool
type Pool[T any] struct {
	// Core worker management - lock-free where possible
	workers  []*worker[T]    // Pre-allocated worker slice
	workerCh chan *worker[T] // Available workers (acts as semaphore)
	capacity int32           // Pool capacity

	// Result handling
	results    chan Result[T]
	resultPool sync.Pool // Recycle result objects

	// Statistics - all atomic for lock-free access
	running   int64 // Currently executing jobs
	submitted int64 // Total submitted jobs
	completed int64 // Successfully completed jobs
	failed    int64 // Failed jobs

	// Lifecycle management
	closed    int64 // atomic flag
	closeOnce sync.Once
	stopCh    chan struct{}
	wg        sync.WaitGroup

	// Configuration
	maxIdleTime     time.Duration
	cleanupInterval time.Duration
}
