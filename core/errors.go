package core

import (
	"errors"
	"fmt"
	"strings"
)

type AggregateError struct {
	Errors []error //  original errors slice
	joined error   // wrapped errors.Join result
}

// Error returns a human-friendly multi-line error message.
func (a AggregateError) Error() string {
	if len(a.Errors) == 0 {
		return "no errors"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d errors occurred:\n", len(a.Errors))
	for i, e := range a.Errors {
		fmt.Fprintf(&b, "  %d) %v\n", i+1, e)
	}
	return b.String()
}

// Unwrap makes AggregateError compatible with errors.Is / errors.As.
// It unwraps to errors.Join(a.Errors...).
func (a AggregateError) Unwrap() error {
	return a.joined
}

// NewAggregateError constructs an AggregateError from a slice of errors.
func NewAggregateError(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0] // just return the single error directly
	}
	return AggregateError{
		Errors: errs,
		joined: errors.Join(errs...),
	}
}

// PanicError wraps a recovered panic value into an error.
type PanicError struct {
	Value interface{}
}

func (p PanicError) Error() string {
	return fmt.Sprintf("panic recovered: %v", p.Value)
}
