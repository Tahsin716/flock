package pool

import (
	"sync"
	"time"
)

// Task represents a unit of work
type Task func()

// Pool is a simple, fast worker pool
type Pool struct {
	// Configuration (immutable after creation)
	capacity       int32
	expiryDuration time.Duration
	nonblocking    bool

	// Worker management
	workers chan *worker
	running int32

	// State management
	state int32 // 0: closed, 1: running

	// Statistics
	submitted uint64
	completed uint64

	// Cleanup
	stopCleanup chan struct{}
	wg          sync.WaitGroup
}

// worker executes tasks
type worker struct {
	pool     *Pool
	task     chan Task
	lastUsed time.Time
}

// Pool State
const (
	StateClosed int32 = iota
	StateRunning
)
