package flock

import (
	"time"
)

// Config contains all configuration options for the worker pool
type Config struct {
	// NumWorkers is the number of worker goroutines
	// If 0, defaults to runtime.NumCPU()
	NumWorkers int

	// QueueSizePerWorker is the initial size of each worker's deque (Must be a power of 2)
	// The deque grows dynamically up to maxDequeCapacity (65536)
	// If 0, defaults to 256
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
}

// DefaultConfig returns a Config with production-ready defaults
func DefaultConfig() Config {
	return Config{
		NumWorkers:         0, // runtime.NumCPU()
		QueueSizePerWorker: 256,
		PanicHandler:       nil,
		MaxParkTime:        10 * time.Millisecond,
		SpinCount:          30,
		PinWorkerThreads:   false,
	}
}

// Validate checks the configuration and returns an error if invalid
func (c *Config) Validate() error {
	if c.NumWorkers < 0 {
		return ErrInvalidConfig("NumWorkers must be >= 0")
	}

	if c.NumWorkers > 10000 {
		return ErrInvalidConfig("NumWorkers too large (>10000), likely a mistake")
	}

	if c.QueueSizePerWorker <= 0 {
		return ErrInvalidConfig("QueueSizePerWorker must be positive")
	}

	if c.QueueSizePerWorker&(c.QueueSizePerWorker-1) != 0 {
		return ErrInvalidConfig("QueueSizePerWorker must be a power of 2")
	}

	if c.QueueSizePerWorker > 100000 {
		return ErrInvalidConfig("QueueSizePerWorker too large (>100000), use dynamic sizing")
	}

	if c.MaxParkTime <= 0 {
		return ErrInvalidConfig("MaxParkTime must be positive")
	}

	if c.MaxParkTime > 1*time.Minute {
		return ErrInvalidConfig("MaxParkTime too large (>1min), workers may appear stuck")
	}

	if c.SpinCount < 0 {
		return ErrInvalidConfig("SpinCount must be >= 0")
	}

	if c.SpinCount > 10000 {
		return ErrInvalidConfig("SpinCount too large (>10000), will waste CPU")
	}

	return nil
}
