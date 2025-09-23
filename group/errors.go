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
