package group

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

func TestFailFastMode(t *testing.T) {
	g := New(WithErrorMode(FailFast))

	var wg sync.WaitGroup
	started := int32(0)
	completed := int32(0)

	// Add multiple goroutines
	for i := 0; i < 5; i++ {
		i := i
		wg.Add(1)
		g.Go(func(ctx context.Context) error {
			defer wg.Done()
			atomic.AddInt32(&started, 1)

			// First goroutine fails immediately
			if i == 0 {
				return fmt.Errorf("error from goroutine %d", i)
			}

			// Others check for cancellation
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
				atomic.AddInt32(&completed, 1)
				return nil
			}
		})
	}

	err := g.Wait()
	wg.Wait() // Wait for all goroutines to actually finish

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Should get the first error or context cancellation
	if !strings.Contains(err.Error(), "error from goroutine 0") && !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("Unexpected error: %v", err)
	}

	// Context should be cancelled, so not all goroutines should complete normally
	if atomic.LoadInt32(&completed) >= 4 {
		t.Errorf("Expected fewer completions due to cancellation, got %d", atomic.LoadInt32(&completed))
	}
}
