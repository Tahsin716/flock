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

func TestIgnoreErrorsMode(t *testing.T) {
	g := New(WithErrorMode(IgnoreErrors))

	// Add goroutines that return errors
	for i := 0; i < 3; i++ {
		i := i
		g.Go(func(ctx context.Context) error {
			return fmt.Errorf("error %d", i)
		})
	}

	err := g.Wait()
	if err != nil {
		t.Errorf("Expected no error in IgnoreErrors mode, got: %v", err)
	}

	// Check stats - errors should still be counted as failed
	stats := g.Stats()
	if stats.Failed != 3 {
		t.Errorf("Expected 3 failed goroutines, got %d", stats.Failed)
	}
}

func TestPanicRecovery(t *testing.T) {
	g := New(WithErrorMode(CollectAll))

	// Add goroutine that panics
	g.Go(func(ctx context.Context) error {
		panic("test panic")
	})

	// Add normal goroutine
	g.Go(func(ctx context.Context) error {
		return nil
	})

	err := g.Wait()
	if err == nil {
		t.Fatal("Expected error from panic, got nil")
	}

	// Should contain panic information
	if !strings.Contains(err.Error(), "panic: test panic") {
		t.Errorf("Expected panic error, got: %v", err)
	}

	// Check stats
	stats := g.Stats()
	if stats.Failed != 1 {
		t.Errorf("Expected 1 failed goroutine, got %d", stats.Failed)
	}
}

func TestPanicInFailFastMode(t *testing.T) {
	g := New(WithErrorMode(FailFast))

	var wg sync.WaitGroup

	// Add goroutine that panics
	wg.Add(1)
	g.Go(func(ctx context.Context) error {
		defer wg.Done()
		panic("test panic")
	})

	// Add goroutine that should be cancelled
	wg.Add(1)
	g.Go(func(ctx context.Context) error {
		defer wg.Done()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			return nil
		}
	})

	err := g.Wait()
	wg.Wait()

	if err == nil {
		t.Fatal("Expected error from panic, got nil")
	}

	// Should be either the panic error or context cancellation error
	if !strings.Contains(err.Error(), "panic") && !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("Unexpected error type: %v", err)
	}
}

func TestMultipleWait(t *testing.T) {
	g := New()

	g.Go(func(ctx context.Context) error {
		return errors.New("test error")
	})

	// First Wait call
	err1 := g.Wait()
	if err1 == nil {
		t.Error("Expected error from first Wait call")
	}

	// Second Wait call should return the same result
	err2 := g.Wait()
	if err2 == nil {
		t.Error("Expected error from second Wait call")
	}

	// Both should contain the same error
	if err1.Error() != err2.Error() {
		t.Errorf("Multiple Wait() calls should return consistent results: %v != %v", err1, err2)
	}
}
