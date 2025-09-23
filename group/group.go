package group

import (
	"context"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// Group manages a collection of goroutines with structured concurrency
type Group struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	config Config

	// Error handling
	errors    []error
	errorsMux sync.RWMutex
	failOnce  sync.Once
	firstErr  atomic.Value // used in FailFast
}

// NewGroup creates a new Group with the given options
func NewGroup(opts ...Option) *Group {
	return NewGroupWithContext(context.Background(), opts...)
}

// NewGroupWithContext creates a new Group with a parent context
func NewGroupWithContext(ctx context.Context, opts ...Option) *Group {
	config := BuildConfig(opts)

	if ctx == nil {
		ctx = context.Background()
	}

	groupCtx, cancel := context.WithCancel(ctx)

	return &Group{
		ctx:    groupCtx,
		cancel: cancel,
		config: config,
		errors: make([]error, 0),
	}
}

// Go runs a new function in a new goroutine with panic recovery
func (g *Group) Go(fn func(context.Context) error) {
	g.wg.Add(1)

	go func() {
		defer g.wg.Done()

		// Handle panics
		defer func() {
			if r := recover(); r != nil {
				panicErr := &PanicError{
					Value: r,
					Stack: string(debug.Stack()),
				}
				g.handleError(panicErr)
			}
		}()

		// Run the function
		if err := fn(g.ctx); err != nil {
			g.handleError(err)
		}
	}()
}

// GoSafe runs a function in a new goroutine, ignoring errors and panics
// This is for fire-and-forget tasks
func (g *Group) GoSafe(fn func(context.Context)) {
	g.Go(func(ctx context.Context) error {
		fn(ctx)
		return nil
	})
}

// Wait waits for all goroutines to complete and returns any errors.
func (g *Group) Wait() error {
	g.wg.Wait()
	g.Stop()

	switch g.config.errorMode {
	case IgnoreErrors:
		return nil

	case FailFast:
		if v := g.firstErr.Load(); v != nil {
			return v.(error)
		}
		return nil

	case CollectAll:
		g.errorsMux.RLock()
		collectedErrors := make([]error, len(g.errors))
		copy(collectedErrors, g.errors)
		g.errorsMux.RUnlock()

		if len(collectedErrors) > 0 {
			return AggregateError{Errors: collectedErrors}
		}
		return nil

	default:
		return nil
	}
}

// Stop cancels the group context, signaling all goroutines to stop
func (g *Group) Stop() {
	g.cancel()
}

// handleError processes an error according to the error mode
func (g *Group) handleError(err error) {
	switch g.config.errorMode {
	case IgnoreErrors:
		return

	case FailFast:
		if g.firstErr.Load() == nil {
			if g.firstErr.CompareAndSwap(nil, err) {
				g.failOnce.Do(func() {
					g.cancel()
				})
			}
		}

	case CollectAll:
		g.errorsMux.Lock()
		g.errors = append(g.errors, err)
		g.errorsMux.Unlock()
	}
}
