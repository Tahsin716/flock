package group

import (
	"context"
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
