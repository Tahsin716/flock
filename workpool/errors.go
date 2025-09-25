package workpool

import (
	"errors"
	"fmt"
)

// PanicError wraps a recovered panic
type PanicError struct {
	Value interface{}
	Stack string
}

func (p *PanicError) Error() string {
	return fmt.Sprintf("panic: %v\n%s", p.Value, p.Stack)
}

// Common errors
var (
	ErrPoolClosed = errors.New("pool is closed")
	ErrPoolFull   = errors.New("no workers available")
	ErrWorkerBusy = errors.New("worker is busy")
)
