package group

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCollectAllMode(t *testing.T) {
	g := New(WithErrorMode(CollectAll))

	// Add goroutines that return errors
	expectedErrors := []string{"error 1", "error 2", "error 3"}
	for _, errMsg := range expectedErrors {
		errMsg := errMsg // Capture loop variable
		g.Go(func(ctx context.Context) error {
			return errors.New(errMsg)
		})
	}

	// Add successful goroutine
	g.Go(func(ctx context.Context) error {
		return nil
	})

	err := g.Wait()
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Check that error contains all expected errors
	errStr := err.Error()
	for _, expected := range expectedErrors {
		if !strings.Contains(errStr, expected) {
			t.Errorf("Expected error %q not found in result: %v", expected, errStr)
		}
	}

	// Check stats
	stats := g.Stats()
	if stats.Failed != 3 {
		t.Errorf("Expected 3 failed goroutines, got %d", stats.Failed)
	}
	if stats.Completed != 4 {
		t.Errorf("Expected 4 completed goroutines, got %d", stats.Completed)
	}
}
