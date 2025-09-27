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

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.errorMode != CollectAll {
		t.Errorf("Expected default error mode %v, got %v", CollectAll, config.errorMode)
	}
}

func TestWithErrorMode(t *testing.T) {
	tests := []struct {
		name string
		mode ErrorMode
	}{
		{"CollectAll", CollectAll},
		{"FailFast", FailFast},
		{"IgnoreErrors", IgnoreErrors},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := New(WithErrorMode(tt.mode))
			if g.config.errorMode != tt.mode {
				t.Errorf("Expected error mode %v, got %v", tt.mode, g.config.errorMode)
			}
		})
	}
}

func TestNewWithOptions(t *testing.T) {
	tests := []struct {
		name     string
		opts     []Option
		expected Config
	}{
		{
			name: "no options",
			opts: []Option{},
			expected: Config{
				errorMode: CollectAll,
			},
		},
		{
			name: "fail fast mode",
			opts: []Option{WithErrorMode(FailFast)},
			expected: Config{
				errorMode: FailFast,
			},
		},
		{
			name: "ignore errors mode",
			opts: []Option{WithErrorMode(IgnoreErrors)},
			expected: Config{
				errorMode: IgnoreErrors,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := New(tt.opts...)
			if g.config.errorMode != tt.expected.errorMode {
				t.Errorf("Expected error mode %v, got %v", tt.expected.errorMode, g.config.errorMode)
			}
		})
	}
}

func TestPanicError(t *testing.T) {
	panicErr := &PanicError{
		Value: "test panic",
		Stack: "stack trace here",
	}

	errStr := panicErr.Error()
	if !strings.Contains(errStr, "panic: test panic") {
		t.Errorf("PanicError string should contain panic value: %s", errStr)
	}

	if !strings.Contains(errStr, "stack trace here") {
		t.Errorf("PanicError string should contain stack trace: %s", errStr)
	}
}

func TestPanicErrorWithDifferentTypes(t *testing.T) {
	tests := []struct {
		name       string
		panicValue interface{}
	}{
		{"string panic", "string panic"},
		{"int panic", 42},
		{"nil panic", nil},
		{"struct panic", struct{ msg string }{"test"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			panicErr := &PanicError{
				Value: tt.panicValue,
				Stack: "test stack",
			}

			errStr := panicErr.Error()
			if !strings.Contains(errStr, "panic:") {
				t.Errorf("Expected panic prefix in error string: %s", errStr)
			}
		})
	}
}

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

func TestRealWorldScenario(t *testing.T) {
	// Simulate processing a batch of work items
	g := New(WithErrorMode(CollectAll))

	workItems := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	processed := int32(0)
	failed := int32(0)

	for _, item := range workItems {
		item := item
		g.Go(func(ctx context.Context) error {
			// Simulate some work
			select {
			case <-time.After(time.Duration(item) * time.Millisecond):
				// Item 5 fails
				if item == 5 {
					atomic.AddInt32(&failed, 1)
					return fmt.Errorf("failed to process item %d", item)
				}
				atomic.AddInt32(&processed, 1)
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}

	err := g.Wait()

	// Should have one error (item 5)
	if err == nil {
		t.Fatal("Expected error from failed item")
	}

	// 9 items should have processed successfully
	if atomic.LoadInt32(&processed) != 9 {
		t.Errorf("Expected 9 processed items, got %d", atomic.LoadInt32(&processed))
	}

	if atomic.LoadInt32(&failed) != 1 {
		t.Errorf("Expected 1 failed item, got %d", atomic.LoadInt32(&failed))
	}
}

func TestTimeoutScenario(t *testing.T) {
	// Create group with short timeout
	g := NewWithTimeout(50*time.Millisecond, WithErrorMode(FailFast))

	completed := int32(0)

	// Add tasks that take longer than timeout
	for i := 0; i < 3; i++ {
		g.Go(func(ctx context.Context) error {
			select {
			case <-time.After(100 * time.Millisecond):
				atomic.AddInt32(&completed, 1)
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}

	start := time.Now()
	err := g.Wait()
	duration := time.Since(start)

	// Should complete quickly due to timeout
	if duration > 100*time.Millisecond {
		t.Errorf("Wait took too long: %v", duration)
	}

	// Should get a context deadline exceeded error
	if err == nil {
		t.Fatal("Expected timeout error")
	}

	// Tasks shouldn't have completed normally due to timeout
	if atomic.LoadInt32(&completed) > 0 {
		t.Errorf("Expected no normal completions due to timeout, got %d", atomic.LoadInt32(&completed))
	}
}

func TestGracefulShutdown(t *testing.T) {
	g := New(WithErrorMode(CollectAll))

	started := int32(0)
	finished := int32(0)

	// Add long-running tasks
	for i := 0; i < 5; i++ {
		g.Go(func(ctx context.Context) error {
			atomic.AddInt32(&started, 1)
			defer atomic.AddInt32(&finished, 1)

			select {
			case <-time.After(1 * time.Second):
				return nil
			case <-ctx.Done():
				// Simulate cleanup work
				time.Sleep(10 * time.Millisecond)
				return ctx.Err()
			}
		})
	}

	// Wait for all to start
	for atomic.LoadInt32(&started) < 5 {
		time.Sleep(time.Millisecond)
	}

	// Initiate graceful shutdown
	g.Stop()

	start := time.Now()
	err := g.Wait()
	duration := time.Since(start)

	// Should complete quickly due to cancellation
	if duration > 100*time.Millisecond {
		t.Errorf("Graceful shutdown took too long: %v", duration)
	}

	// Should get cancellation errors
	if err == nil {
		t.Fatal("Expected cancellation error")
	}

	// All tasks should have finished (either normally or via cancellation)
	if atomic.LoadInt32(&finished) != 5 {
		t.Errorf("Expected all 5 tasks to finish, got %d", atomic.LoadInt32(&finished))
	}
}

func TestHighConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping high concurrency test in short mode")
	}

	g := New(WithErrorMode(CollectAll))

	const numGoroutines = 1000000
	counter := int32(0)

	// Launch many goroutines
	for i := 0; i < numGoroutines; i++ {
		g.Go(func(ctx context.Context) error {
			atomic.AddInt32(&counter, 1)
			// Small random delay to increase race conditions
			time.Sleep(time.Microsecond * time.Duration(counter%10))
			return nil
		})
	}

	err := g.Wait()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if atomic.LoadInt32(&counter) != numGoroutines {
		t.Errorf("Expected %d executions, got %d", numGoroutines, atomic.LoadInt32(&counter))
	}

	stats := g.Stats()
	if stats.Completed != numGoroutines {
		t.Errorf("Expected %d completed, got %d", numGoroutines, stats.Completed)
	}
}
