package pool

import (
	"errors"
	"fmt"
)

// PanicError wraps a recovered panic value and its stack trace.
type PanicError struct {
	Value interface{}
	Stack string
}

// Error implements the error interface for PanicError.
func (p *PanicError) Error() string {
	return fmt.Sprintf("panic: %v\n%s", p.Value, p.Stack)
}

// Common errors returned by the pool.
var (
	ErrPoolClosed = errors.New("pool is closed")
	ErrPoolFull   = errors.New("no workers available")
	ErrWorkerBusy = errors.New("worker is busy")
)
