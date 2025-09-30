package pool

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
