package group

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

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
