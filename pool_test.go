package flock

import (
	"runtime"
	"testing"
	"time"
)

// ============================================================================
// Pool Creation Tests
// ============================================================================

func TestNewPool_DefaultConfig(t *testing.T) {
	pool, err := NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	if pool.NumWorkers() != runtime.NumCPU() {
		t.Errorf("Expected %d workers, got %d", runtime.NumCPU(), pool.NumWorkers())
	}
}

func TestNewPool_WithOptions(t *testing.T) {
	pool, err := NewPool(
		WithNumWorkers(4),
		WithQueueSizePerWorker(128),
		WithSpinCount(10),
		WithMaxParkTime(5*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Shutdown(false)

	if pool.NumWorkers() != 4 {
		t.Errorf("Expected 4 workers, got %d", pool.NumWorkers())
	}
}

func TestNewPool_InvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
	}{
		{
			name: "negative workers",
			opts: []Option{WithNumWorkers(-1)},
		},
		{
			name: "zero queue size",
			opts: []Option{WithQueueSizePerWorker(0)},
		},
		{
			name: "non-power-of-2 queue",
			opts: []Option{WithQueueSizePerWorker(100)},
		},
		{
			name: "negative spin count",
			opts: []Option{WithSpinCount(-1)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPool(tt.opts...)
			if err == nil {
				t.Error("Expected error, got nil")
			}
		})
	}
}
