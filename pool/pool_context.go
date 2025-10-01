package pool

import "context"

type ContextPool struct {
	*Pool
	ctx context.Context
}

func NewContextPool(ctx context.Context, capacity int) *ContextPool {
	return &ContextPool{
		Pool: New(capacity),
		ctx:  ctx,
	}
}

// GoCtx submits a task with context
func (cp *ContextPool) GoCtx(task func(context.Context) error) error {
	return cp.Go(func() {
		select {
		case <-cp.ctx.Done():
			return
		default:
			task(cp.ctx)
		}
	})
}
