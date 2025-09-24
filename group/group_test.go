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
