package flock

import "fmt"

// Common errors
var (
	// ErrPoolShutdown is returned when trying to submit to a shutdown pool
	ErrPoolShutdown = &PoolError{msg: "pool is shutdown"}

	// ErrQueueFull is returned when queue is full and strategy is ReturnError
	ErrQueueFull = &PoolError{msg: "queue is full"}

	// ErrTimeout is returned when an operation times out
	ErrTimeout = &PoolError{msg: "operation timed out"}

	// ErrNilTask is returned when an task submitted is nil
	ErrNilTask = &PoolError{msg: "task is nil"}
)

// PoolError represents an error from the worker pool
type PoolError struct {
	msg string
	err error
}

func (e *PoolError) Error() string {
	if e.err != nil {
		return fmt.Sprintf("workerpool: %s: %v", e.msg, e.err)
	}
	return fmt.Sprintf("workerpool: %s", e.msg)
}

func (e *PoolError) Unwrap() error {
	return e.err
}

// errrInvalidConfig creates an error for invalid configuration
func errInvalidConfig(msg string) error {
	return &PoolError{msg: "invalid config: " + msg}
}

// errWorker creates an error for worker-related issues
func errWorker(workerID int, err error) error {
	return &PoolError{
		msg: fmt.Sprintf("worker %d error", workerID),
		err: err,
	}
}
