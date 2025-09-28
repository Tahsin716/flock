package workpool

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// Helper function to create a simple job for testing.
func newTestJob(duration time.Duration, shouldError bool) Job[bool] {
	return func(ctx context.Context) (bool, error) {
		select {
		case <-time.After(duration):
			if shouldError {
				return false, context.DeadlineExceeded
			}
			return true, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

func TestNewPool(t *testing.T) {
	t.Run("DefaultConfig", func(t *testing.T) {
		p := New[bool]()
		defer p.Close()
		config := DefaultConfig()
		if p.minWorkers != int32(config.MinWorkers) || p.maxWorkers != int32(config.MaxWorkers) {
			t.Errorf("New() with default config failed, got min:%d max:%d", p.minWorkers, p.maxWorkers)
		}
		if p.Stats().Running != 0 || atomic.LoadInt32(&p.currentWorkers) != p.minWorkers {
			t.Errorf("Initial workers count is incorrect")
		}
	})

	t.Run("WithCustomOptions", func(t *testing.T) {
		p := New[bool](WithMinWorkers(5), WithMaxWorkers(10))
		defer p.Close()
		if p.minWorkers != 5 || p.maxWorkers != 10 {
			t.Errorf("New() with custom options failed, got min:%d max:%d", p.minWorkers, p.maxWorkers)
		}
	})

	t.Run("ValidationLogic", func(t *testing.T) {
		p := New[bool](WithMinWorkers(10), WithMaxWorkers(5)) // min > max
		defer p.Close()
		if p.minWorkers != 5 || p.maxWorkers != 5 {
			t.Errorf("Validation for min > max failed, got min:%d max:%d", p.minWorkers, p.maxWorkers)
		}
	})
}
