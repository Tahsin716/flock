package core

import (
	"context"
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
	errors    chan error
	errorOnce sync.Once
	closeOnce sync.Once

	// State tracking
	running   int64
	completed int64
	failed    int64
}

// Stats provides information about goroutine execution
type Stats struct {
	Running   int64
	Completed int64
	Failed    int64
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

	g := &Group{
		ctx:    groupCtx,
		cancel: cancel,
		config: config,
		errors: make(chan error, config.errorBuffer),
	}

	return g
}

// NewGroupWithTimeout creates a Group with a timeout
func NewGroupWithTimeout(timeout time.Duration, opts ...Option) *Group {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	g := NewGroupWithContext(ctx, opts...)
	g.cancel = cancel // Override to use the timeout cancel
	return g
}

// NewGroupWithDeadline creates a Group with a deadline
func NewGroupWithDeadline(deadline time.Time, opts ...Option) *Group {
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	g := NewGroupWithContext(ctx, opts...)
	g.cancel = cancel // Override to use the deadline cancel
	return g
}

// Go runs a new function in new goroutine with panic recovery
func (g *Group) Go(fn func(context.Context) error) error {
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

	return nil
}

// GoSafe runs a function in a new goroutine, ignoring errors and panics
// This is for fire-and-forget tasks
func (g *Group) GoSafe(fn func(context.Context)) error {
	return g.Go(func(ctx context.Context) error {
		fn(ctx)
		return nil
	})
}

// Wait waits for all goroutines to complete and returns any errors
func (g *Group) Wait() error {
	g.wg.Wait()
	g.Stop()

	// Close errors channel to signal no more errors
	g.closeOnce.Do(func() {
		close(g.errors)
	})

	if g.config.errorMode == IgnoreErrors {
		// Drain the channel but ignore errors
		for range g.errors {
		}
		return nil
	}

	// Collect errors
	var collectedErrors []error
	for err := range g.errors {
		collectedErrors = append(collectedErrors, err)
		if g.config.errorMode == FailFast {
			return err // Return first error immediately
		}
	}

	// Return aggregate error if we have errors and we're collecting all
	if len(collectedErrors) > 0 && g.config.errorMode == CollectAll {
		return &AggregateError{Errors: collectedErrors}
	}

	return nil
}

// Stop cancels the group context, signaling all goroutines to stop
func (g *Group) Stop() {
	g.cancel()
}

// Stats returns current statistics about the group
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
		// Send error and cancel (but only once)
		g.errorOnce.Do(func() {
			select {
			case g.errors <- err:
			default:
				// Buffer full, but we're canceling anyway
			}
			g.cancel()
		})
	case CollectAll:
		select {
		case g.errors <- err:
		default:
			// Buffer full - could log this or handle differently
		}
	}
}
