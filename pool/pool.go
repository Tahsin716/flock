// Package workpool provides high-performance, resilient, and elastic goroutine pools.
package pool

import (
	"context"
	"sync"
	"time"
)

// Task represents a unit of work with no return value.
// Users handle results/errors within the task itself.
type Task func()

// TaskWithContext is a task that accepts a context.
type TaskWithContext func(ctx context.Context)

// Pool is a high-performance worker pool inspired by ants.
type Pool struct {
	// Configuration
	capacity       int32
	expiryDuration time.Duration

	// Core components
	workers workerStack
	lock    sync.Locker
	cond    *sync.Cond

	// State
	state   int32 // 0: running, 1: closed
	running int32 // currently running workers
	waiting int32 // workers waiting for tasks

	// Statistics
	submitted uint64
	completed uint64

	// Lifecycle
	once        sync.Once
	stopCleanup chan struct{}

	// Options
	nonblocking bool
	preAlloc    bool
}

// PoolWithFunc is a pool that accepts tasks with a specific function signature.
type PoolWithFunc struct {
	*Pool
	poolFunc func(interface{})
}
