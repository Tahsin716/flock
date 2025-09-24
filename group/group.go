package group

import (
	"context"
	"errors"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
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

	// Stats
	running   int64
	completed int64
	failed    int64
}

// Stats represents current state of the Group
type Stats struct {
	Running   int64
	Completed int64
	Failed    int64
}

// New creates a new Group with options
func New(opts ...Option) *Group {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Group{
		ctx:    ctx,
		cancel: cancel,
		config: config,
		errors: make([]error, 0),
	}
}

// NewWithContext creates a new Group with a parent context
func NewWithContext(ctx context.Context, opts ...Option) *Group {
	if ctx == nil {
		ctx = context.Background()
	}

	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	groupCtx, cancel := context.WithCancel(ctx)

	return &Group{
		ctx:    groupCtx,
		cancel: cancel,
		config: config,
		errors: make([]error, 0),
	}
}

// NewWithTimeout creates a Group with a timeout
func NewWithTimeout(timeout time.Duration, opts ...Option) *Group {
	if timeout <= 0 {
		timeout = time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	g := NewWithContext(ctx, opts...)
	g.cancel = cancel // Override to use timeout cancel
	return g
}

// NewWithDeadline creates a Group with a deadline
func NewWithDeadline(deadline time.Time, opts ...Option) *Group {
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	g := NewWithContext(ctx, opts...)
	g.cancel = cancel // Override to use deadline cancel
	return g
}

// Go runs a new function in a new goroutine with panic recovery
func (g *Group) Go(fn func(context.Context) error) {
	atomic.AddInt64(&g.running, 1)
	g.wg.Add(1)

	go func() {
		defer func() {
			atomic.AddInt64(&g.running, -1)
			atomic.AddInt64(&g.completed, 1)
			g.wg.Done()
		}()

		// Handle panics
		defer func() {
			if r := recover(); r != nil {
				atomic.AddInt64(&g.failed, 1)
				panicErr := &PanicError{
					Value: r,
					Stack: string(debug.Stack()),
				}
				g.handleError(panicErr)
			}
		}()

		// Run the function
		if err := fn(g.ctx); err != nil {
			atomic.AddInt64(&g.failed, 1)
			g.handleError(err)
		}
	}()
}

// GoSafe runs a function in a new goroutine with panic recovery.
// Unlike Go(), this participates in Wait() but ignores function panics/errors.
func (g *Group) GoSafe(fn func()) {
	atomic.AddInt64(&g.running, 1)
	g.wg.Add(1)

	go func() {
		defer func() {
			atomic.AddInt64(&g.running, -1)
			atomic.AddInt64(&g.completed, 1)
			g.wg.Done()
		}()

		// Handle panics silently
		defer func() {
			if r := recover(); r != nil {
				atomic.AddInt64(&g.failed, 1)
				// Panic recovered, but we don't propagate it
				// This is "safe" mode - fire and forget
			}
		}()

		fn()
	}()
}

// Wait waits for all goroutines to complete and returns any errors
func (g *Group) Wait() error {
	g.wg.Wait()
	g.cancel() // Cancel context after all goroutines complete

	switch g.config.errorMode {
	case IgnoreErrors:
		return nil

	case FailFast:
		g.errorsMux.RLock()
		defer g.errorsMux.RUnlock()
		if len(g.errors) > 0 {
			return g.errors[0] // Return first error that occurred
		}
		return nil

	case CollectAll:
		g.errorsMux.RLock()
		defer g.errorsMux.RUnlock()
		if len(g.errors) > 0 {
			return errors.Join(g.errors...)
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

// Context returns the group's context
func (g *Group) Context() context.Context {
	return g.ctx
}

// Stats returns current statistics about the Group
func (g *Group) Stats() Stats {
	return Stats{
		Running:   atomic.LoadInt64(&g.running),
		Completed: atomic.LoadInt64(&g.completed),
		Failed:    atomic.LoadInt64(&g.failed),
	}
}

// handleError processes an error according to the error mode
func (g *Group) handleError(err error) {
	switch g.config.errorMode {
	case IgnoreErrors:
		return

	case FailFast:
		// Store error and cancel on first error only
		g.failOnce.Do(func() {
			g.errorsMux.Lock()
			g.errors = append(g.errors, err)
			g.errorsMux.Unlock()
			g.cancel()
		})

	case CollectAll:
		// Always store the error
		g.errorsMux.Lock()
		g.errors = append(g.errors, err)
		g.errorsMux.Unlock()
	}
}
