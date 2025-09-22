package core

import (
	"errors"
	"fmt"
	"strings"
)

// AggregateError combines multiple errors
type AggregateError struct {
	Errors []error
}

func (a *AggregateError) Error() string {
	if len(a.Errors) == 0 {
		return "no errors"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d error(s) occurred:", len(a.Errors))
	for i, err := range a.Errors {
		fmt.Fprintf(&b, "\n  [%d] %v", i+1, err)
	}
	return b.String()
}

// Unwrap makes AggregateError compatible with errors.Is/errors.As
func (a *AggregateError) Unwrap() []error {
	return a.Errors
}

// Is implements error matching for wrapped errors
func (a *AggregateError) Is(target error) bool {
	for _, err := range a.Errors {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// As implements error unwrapping for wrapped errors
func (a *AggregateError) As(target interface{}) bool {
	for _, err := range a.Errors {
		if errors.As(err, target) {
			return true
		}
	}
	return false
}

// PanicError wraps a recovered panic
type PanicError struct {
	Value interface{}
	Stack string
}

func (p *PanicError) Error() string {
	return fmt.Sprintf("panic: %v", p.Value)
}
