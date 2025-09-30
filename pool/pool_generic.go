package pool

import "time"

// Generic pool with function
type PoolWithFuncGeneric[T any] struct {
	pool *Pool
	fn   func(T)
}

// NewPoolWithFuncGeneric creates a generic pool
func NewPoolWithFuncGeneric[T any](size int, fn func(T), opts ...Option) (*PoolWithFuncGeneric[T], error) {
	if fn == nil {
		return nil, ErrFuncNil
	}

	pool, err := NewPool(size, opts...)
	if err != nil {
		return nil, err
	}

	return &PoolWithFuncGeneric[T]{
		pool: pool,
		fn:   fn,
	}, nil
}

// Invoke submits a typed task
func (p *PoolWithFuncGeneric[T]) Invoke(arg T) error {
	return p.pool.Submit(func() {
		p.fn(arg)
	})
}

// Running returns running workers
func (p *PoolWithFuncGeneric[T]) Running() int { return p.pool.Running() }

// Free returns free slots
func (p *PoolWithFuncGeneric[T]) Free() int { return p.pool.Free() }

// Cap returns capacity
func (p *PoolWithFuncGeneric[T]) Cap() int { return p.pool.Cap() }

// Submitted returns submitted count
func (p *PoolWithFuncGeneric[T]) Submitted() uint64 { return p.pool.Submitted() }

// Completed returns completed count
func (p *PoolWithFuncGeneric[T]) Completed() uint64 { return p.pool.Completed() }

// Tune changes capacity
func (p *PoolWithFuncGeneric[T]) Tune(size int) { p.pool.Tune(size) }

// Release closes the pool
func (p *PoolWithFuncGeneric[T]) Release() { p.pool.Release() }

// ReleaseTimeout closes with timeout
func (p *PoolWithFuncGeneric[T]) ReleaseTimeout(timeout time.Duration) error {
	return p.pool.ReleaseTimeout(timeout)
}

// Reboot restarts the pool
func (p *PoolWithFuncGeneric[T]) Reboot() { p.pool.Reboot() }

// IsClosed checks if closed
func (p *PoolWithFuncGeneric[T]) IsClosed() bool { return p.pool.IsClosed() }
