package core

import (
	"context"
	"sync"
)

type Group struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	errs   chan error
	opts   groupOptions
}

// NewGroup creates a new Group with default options.
func NewGroup() *Group {
	return NewGroupWithOptions(DefaultOptions())
}

// NewGroupWithOptions creates a new Group with custom options.
func NewGroupWithOptions(opts groupOptions) *Group {
	ctx, cancel := context.WithCancel(context.Background())
	return &Group{
		ctx:    ctx,
		cancel: cancel,
		errs:   make(chan error, opts.bufferSize),
		opts:   opts,
	}
}

// Go runs a function in a new goroutine with panic recovery.
func (g *Group) Go(fn func(ctx context.Context) error) {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				g.errs <- PanicError{Value: r}
				if g.opts.errorMode == FailFast {
					g.cancel()
				}
			}
		}()

		if err := fn(g.ctx); err != nil && g.opts.errorMode != IgnoreErrors {
			g.errs <- err
			if g.opts.errorMode == FailFast {
				g.cancel()
			}
		}
	}()
}

// Wait waits for all goroutines to finish and returns errors based on ErrorMode.
func (g *Group) Wait() error {
	g.wg.Wait()
	close(g.errs)

	var collected []error
	for err := range g.errs {
		collected = append(collected, err)
		if g.opts.errorMode == FailFast {
			return err
		}
	}

	if g.opts.errorMode == CollectAll && len(collected) > 0 {
		return NewAggregateError(collected)
	}

	return nil
}
