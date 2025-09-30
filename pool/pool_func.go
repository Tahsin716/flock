package pool

import "time"

// PoolWithFunc is a pool with a fixed function signature
type PoolWithFunc struct {
	pool *Pool
	fn   func(interface{})
}

// NewPoolWithFunc creates a pool with a fixed function
func NewPoolWithFunc(size int, fn func(interface{}), opts ...Option) (*PoolWithFunc, error) {
	if fn == nil {
		return nil, ErrFuncNil
	}

	pool, err := NewPool(size, opts...)
	if err != nil {
		return nil, err
	}

	return &PoolWithFunc{
		pool: pool,
		fn:   fn,
	}, nil
}

// Invoke submits a task with argument
func (p *PoolWithFunc) Invoke(arg interface{}) error {
	return p.pool.Submit(func() {
		p.fn(arg)
	})
}

// Running returns running workers
func (p *PoolWithFunc) Running() int { return p.pool.Running() }

// Free returns free slots
func (p *PoolWithFunc) Free() int { return p.pool.Free() }

// Cap returns capacity
func (p *PoolWithFunc) Cap() int { return p.pool.Cap() }

// Submitted returns submitted count
func (p *PoolWithFunc) Submitted() uint64 { return p.pool.Submitted() }

// Completed returns completed count
func (p *PoolWithFunc) Completed() uint64 { return p.pool.Completed() }

// Tune changes capacity
func (p *PoolWithFunc) Tune(size int) { p.pool.Tune(size) }

// Release closes the pool
func (p *PoolWithFunc) Release() { p.pool.Release() }

// ReleaseTimeout closes with timeout
func (p *PoolWithFunc) ReleaseTimeout(timeout time.Duration) error {
	return p.pool.ReleaseTimeout(timeout)
}

// Reboot restarts the pool
func (p *PoolWithFunc) Reboot() { p.pool.Reboot() }

// IsClosed checks if closed
func (p *PoolWithFunc) IsClosed() bool { return p.pool.IsClosed() }
