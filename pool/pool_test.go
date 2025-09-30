package pool

import "testing"

// Basic functionality tests

func TestPoolCreation(t *testing.T) {
	pool, err := NewPool(10)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Release()

	if pool.Cap() != 10 {
		t.Errorf("Expected capacity 10, got %d", pool.Cap())
	}
}
