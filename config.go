package flock

import "time"

// OverflowStrategy defines how to handle task submission when queues are full
type OverflowStrategy int

const (
	// Block will block the submitter until space is available
	Block OverflowStrategy = iota
	// DropOldest will drop the oldest task in the queue
	DropOldest
	// DropNewest will drop the incoming task
	DropNewest
	// ReturnError will return an error to the caller
	ReturnError
	// ExecuteCaller will execute the task in the caller's goroutine
	ExecuteCaller
)

// Config contains all configuration options for the worker pool
type Config struct {
	// NumWorkers is the number of worker goroutines
	// If 0, defaults to runtime.NumCPU()
	NumWorkers int

	// QueueSizePerWorker is the size of each worker's local queue
	// Must be a power of 2. If 0, defaults to 1024
	QueueSizePerWorker int

	// OverflowStrategy determines behavior when queues are full
	// Defaults to Block
	OverflowStrategy OverflowStrategy

	// PanicHandler is called when a task panics
	// If nil, panics are logged to stderr
	PanicHandler func(interface{})

	// OnWorkerStart is called when a worker starts
	// Useful for initialization, logging, or tracing
	OnWorkerStart func(workerID int)

	// OnWorkerStop is called when a worker stops
	// Useful for cleanup, logging, or tracing
	OnWorkerStop func(workerID int)

	// PinWorkerThreads attempts to pin workers to CPU cores
	// This can improve cache locality but may reduce flexibility
	// Platform-specific, may have no effect on some systems
	PinWorkerThreads bool

	// MaxParkTime is the maximum time a worker will sleep when idle
	// Defaults to 10ms
	MaxParkTime time.Duration

	// SpinCount is the number of iterations to spin before parking
	// Higher values reduce latency but increase CPU usage when idle
	// Defaults to 30
	SpinCount int
}

// DefaultConfig returns a Config with sensible defaults
func DefaultConfig() Config {
	return Config{
		NumWorkers:         0, // will be set to runtime.NumCPU()
		QueueSizePerWorker: 1024,
		OverflowStrategy:   Block,
		PanicHandler:       nil,
		MaxParkTime:        10 * time.Millisecond,
		SpinCount:          30,
	}
}

// Validate checks the configuration and returns an error if invalid
func (c *Config) Validate() error {
	if c.NumWorkers < 0 {
		return ErrInvalidConfig("NumWorkers must be >= 0")
	}

	if c.QueueSizePerWorker < 0 {
		return ErrInvalidConfig("QueueSizePerWorker must be >= 0")
	}

	if c.QueueSizePerWorker > 0 && !isPowerOfTwo(c.QueueSizePerWorker) {
		return ErrInvalidConfig("QueueSizePerWorker must be a power of 2")
	}

	if c.MaxParkTime < 0 {
		return ErrInvalidConfig("MaxParkTime must be >= 0")
	}

	if c.SpinCount < 0 {
		return ErrInvalidConfig("SpinCount must be >= 0")
	}

	return nil
}

func isPowerOfTwo(n int) bool {
	return n > 0 && (n&(n-1)) == 0
}

func nextPowerOfTwo(n int) int {
	if n <= 0 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n |= n >> 32
	n++
	return n
}
