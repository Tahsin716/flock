package pool

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
