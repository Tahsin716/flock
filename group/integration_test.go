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
