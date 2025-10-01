package pool

import "sync"

type ErrorPool struct {
	*Pool
	mu     sync.Mutex
	errors []error
}

func NewErrorPool(capacity int) *ErrorPool {
	return &ErrorPool{
		Pool:   New(capacity),
		errors: make([]error, 0),
	}
}

// GoE submits a task that returns an error
func (ep *ErrorPool) GoE(task func() error) error {
	return ep.Go(func() {
		if err := task(); err != nil {
			ep.mu.Lock()
			ep.errors = append(ep.errors, err)
			ep.mu.Unlock()
		}
	})
}

// Errors returns collected errors
func (ep *ErrorPool) Errors() []error {
	ep.mu.Lock()
	defer ep.mu.Unlock()

	errs := make([]error, len(ep.errors))
	copy(errs, ep.errors)
	return errs
}
