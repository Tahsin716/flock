package group

import (
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

// AggregateError wraps multiple errors (for CollectAll mode)
type AggregateError struct {
	Errors []error
}

func (a AggregateError) Error() string {
	if len(a.Errors) == 0 {
		return "no errors"
	}
	return fmt.Sprintf("%d errors: %v", len(a.Errors), a.Errors)
}

func (a AggregateError) Unwrap() []error {
	return a.Errors
}
