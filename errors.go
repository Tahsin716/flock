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

// ErrInvalidConfig creates an error for invalid configuration
func ErrInvalidConfig(msg string) error {
	return &PoolError{msg: "invalid config: " + msg}
}

// ErrWorker creates an error for worker-related issues
func ErrWorker(workerID int, err error) error {
	return &PoolError{
		msg: fmt.Sprintf("worker %d error", workerID),
		err: err,
	}
}
