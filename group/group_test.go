package group

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	g := New()
	if g == nil {
		t.Fatal("New() returned nil")
	}

	if g.ctx == nil {
		t.Error("Group context is nil")
	}

	if g.cancel == nil {
		t.Error("Group cancel function is nil")
	}

	// Check default configuration
	if g.config.errorMode != CollectAll {
		t.Errorf("Expected default error mode %v, got %v", CollectAll, g.config.errorMode)
	}
}

func TestNewWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g := NewWithContext(ctx)
	if g == nil {
		t.Fatal("NewWithContext() returned nil")
	}

	g2 := NewWithContext(context.TODO())
	if g2 == nil {
		t.Fatal("NewWithContext(context.TODO()) returned nil")
	}
}

func TestNewWithTimeout(t *testing.T) {
	timeout := 100 * time.Millisecond
	g := NewWithTimeout(timeout)

	deadline, ok := g.ctx.Deadline()
	if !ok {
		t.Error("Context should have deadline")
	}

	if time.Until(deadline) > timeout+10*time.Millisecond {
		t.Error("Deadline is too far in the future")
	}

	// Test with invalid timeout
	g2 := NewWithTimeout(-1 * time.Second)
	if g2 == nil {
		t.Fatal("NewWithTimeout with negative value should still create group")
	}
}

func TestNewWithDeadline(t *testing.T) {
	deadline := time.Now().Add(100 * time.Millisecond)
	g := NewWithDeadline(deadline)

	ctxDeadline, ok := g.ctx.Deadline()
	if !ok {
		t.Error("Context should have deadline")
	}

	// Allow small time difference due to execution time
	if ctxDeadline.Sub(deadline).Abs() > time.Millisecond {
		t.Errorf("Expected deadline %v, got %v", deadline, ctxDeadline)
	}
}

func TestBasicExecution(t *testing.T) {
	g := New()

	executed := int32(0)

	// Add some goroutines
	for i := 0; i < 5; i++ {
		g.Go(func(ctx context.Context) error {
			atomic.AddInt32(&executed, 1)
			return nil
		})
	}

	// Wait for completion
	err := g.Wait()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if atomic.LoadInt32(&executed) != 5 {
		t.Errorf("Expected 5 executions, got %d", atomic.LoadInt32(&executed))
	}

	// Check stats
	stats := g.Stats()
	if stats.Running != 0 {
		t.Errorf("Expected 0 running goroutines, got %d", stats.Running)
	}
	if stats.Completed != 5 {
		t.Errorf("Expected 5 completed goroutines, got %d", stats.Completed)
	}
	if stats.Failed != 0 {
		t.Errorf("Expected 0 failed goroutines, got %d", stats.Failed)
	}
}

func TestGoSafe(t *testing.T) {
	g := New()

	executed := int32(0)

	// GoSafe should not propagate panics
	g.GoSafe(func() {
		atomic.AddInt32(&executed, 1)
	})

	// GoSafe that panics
	g.GoSafe(func() {
		panic("test panic")
	})

	err := g.Wait()
	if err != nil {
		t.Errorf("GoSafe should not propagate errors: %v", err)
	}

	if atomic.LoadInt32(&executed) != 1 {
		t.Errorf("Expected 1 execution, got %d", atomic.LoadInt32(&executed))
	}

	// Check stats - panic should count as failed but completed
	stats := g.Stats()
	if stats.Completed != 2 {
		t.Errorf("Expected 2 completed goroutines, got %d", stats.Completed)
	}
	if stats.Failed != 1 {
		t.Errorf("Expected 1 failed goroutine from panic, got %d", stats.Failed)
	}
}

func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	g := NewWithContext(ctx)

	started := make(chan struct{})
	cancelled := int32(0)

	g.Go(func(ctx context.Context) error {
		close(started)
		select {
		case <-ctx.Done():
			atomic.AddInt32(&cancelled, 1)
			return ctx.Err()
		case <-time.After(1 * time.Second):
			return nil
		}
	})

	// Wait for goroutine to start
	<-started

	// Cancel the context
	cancel()

	err := g.Wait()
	if err == nil {
		t.Fatal("Expected context cancellation error")
	}

	if atomic.LoadInt32(&cancelled) != 1 {
		t.Errorf("Expected 1 cancellation, got %d", atomic.LoadInt32(&cancelled))
	}
}

func TestStop(t *testing.T) {
	g := New()

	stopped := int32(0)

	g.Go(func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			atomic.AddInt32(&stopped, 1)
			return ctx.Err()
		case <-time.After(1 * time.Second):
			return nil
		}
	})

	// Give goroutine time to start
	time.Sleep(10 * time.Millisecond)

	// Stop the group
	g.Stop()

	err := g.Wait()
	if err == nil {
		t.Fatal("Expected context cancellation error")
	}

	if atomic.LoadInt32(&stopped) != 1 {
		t.Errorf("Expected 1 stop, got %d", atomic.LoadInt32(&stopped))
	}
}

func TestContext(t *testing.T) {
	g := New()

	ctx := g.Context()
	if ctx == nil {
		t.Error("Context() returned nil")
	}

	// Context should be the group's context
	if ctx != g.ctx {
		t.Error("Context() should return the group's context")
	}
}

func TestStats(t *testing.T) {
	g := New()

	// Initial stats should be zero
	stats := g.Stats()
	if stats.Running != 0 || stats.Completed != 0 || stats.Failed != 0 {
		t.Errorf("Expected zero stats initially, got %+v", stats)
	}

	ready := make(chan struct{})
	proceed := make(chan struct{})

	// Start a goroutine that waits
	g.Go(func(ctx context.Context) error {
		close(ready)
		<-proceed
		return nil
	})

	// Wait for goroutine to start
	<-ready

	// Check running stats
	stats = g.Stats()
	if stats.Running != 1 {
		t.Errorf("Expected 1 running goroutine, got %d", stats.Running)
	}

	// Let goroutine complete
	close(proceed)

	err := g.Wait()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Check final stats
	stats = g.Stats()
	if stats.Running != 0 || stats.Completed != 1 || stats.Failed != 0 {
		t.Errorf("Expected final stats (0,1,0), got (%d,%d,%d)",
			stats.Running, stats.Completed, stats.Failed)
	}
}
