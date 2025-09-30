package pool

import (
	"sync"
	"sync/atomic"
	"testing"
)

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

func TestPoolSubmit(t *testing.T) {
	pool, _ := NewPool(5)
	defer pool.Release()

	var counter int32
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		err := pool.Submit(func() {
			atomic.AddInt32(&counter, 1)
			wg.Done()
		})
		if err != nil {
			wg.Done()
			t.Errorf("Submit failed: %v", err)
		}
	}

	wg.Wait()

	if counter != 10 {
		t.Errorf("Expected counter=10, got %d", counter)
	}
}

func TestPoolWithPreAlloc(t *testing.T) {
	pool, _ := NewPool(5, WithPreAlloc(true))
	defer pool.Release()

	var counter int32
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		err := pool.Submit(func() {
			atomic.AddInt32(&counter, 1)
			wg.Done()
		})
		if err != nil {
			wg.Done()
			t.Errorf("Submit failed: %v", err)
		}
	}

	wg.Wait()

	if counter != 20 {
		t.Errorf("Expected counter=20, got %d", counter)
	}
}
