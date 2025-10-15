package flock

import "fmt"

// Common errors returned by the worker pool.
var (
	// ErrPoolShutdown is returned when attempting to submit a task to a pool
	// that has been shut down. Once a pool is shutdown, it cannot accept new tasks.
	//
	// Example:
	//  pool.Shutdown(true)
	//  err := pool.Submit(task)
	//  if err == flock.ErrPoolShutdown {
	//      log.Println("Cannot submit: pool is shutdown")
	//  }
	ErrPoolShutdown = &PoolError{msg: "pool is shutdown"}

	// ErrQueueFull is returned when all worker queues are full and the blocking
	// strategy is set to ErrorWhenQueueFull. This allows the caller to implement
	// custom backpressure or retry logic.
	//
	// Example:
	//  pool, _ := flock.NewPool(
	//      flock.WithBlockingStrategy(flock.ErrorWhenQueueFull),
	//  )
	//  err := pool.Submit(task)
	//  if err == flock.ErrQueueFull {
	//      // Implement retry logic or drop task
	//      time.Sleep(100 * time.Millisecond)
	//      pool.Submit(task) // Retry
	//  }
	ErrQueueFull = &PoolError{msg: "queue is full"}

	// ErrTimeout is returned when an operation exceeds its deadline.
	// Currently reserved for future timeout-based operations.
	ErrTimeout = &PoolError{msg: "operation timed out"}

	// ErrNilTask is returned when attempting to submit a nil task function.
	// All submitted tasks must be non-nil function values.
	//
	// Example:
	//  var task func()
	//  err := pool.Submit(task)
	//  if err == flock.ErrNilTask {
	//      log.Println("Cannot submit nil task")
	//  }
	ErrNilTask = &PoolError{msg: "task is nil"}
)

// PoolError represents an error that occurred within the worker pool.
// It wraps underlying errors and provides context about pool operations.
//
// PoolError implements the error interface and supports error unwrapping
// via errors.Unwrap for compatibility with Go 1.13+ error handling.
type PoolError struct {
	msg string // Human-readable error message
	err error  // Underlying error (if any)
}

// Error returns a formatted error message.
// If an underlying error exists, it is included in the output.
func (e *PoolError) Error() string {
	if e.err != nil {
		return fmt.Sprintf("workerpool: %s: %v", e.msg, e.err)
	}
	return fmt.Sprintf("workerpool: %s", e.msg)
}

// Unwrap returns the underlying error, allowing use with errors.Is and errors.As.
//
// Example:
//
//	if errors.Is(err, flock.ErrQueueFull) {
//	    // Handle queue full
//	}
func (e *PoolError) Unwrap() error {
	return e.err
}

// errInvalidConfig creates an error for invalid pool configuration.
// This is returned during pool creation when validation fails.
func errInvalidConfig(msg string) error {
	return &PoolError{msg: "invalid config: " + msg}
}

// errWorker creates an error for worker-related issues.
// Used internally to track which worker encountered an error.
func errWorker(workerID int, err error) error {
	return &PoolError{
		msg: fmt.Sprintf("worker %d error", workerID),
		err: err,
	}
}
