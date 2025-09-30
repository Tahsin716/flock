package pool

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

func TestPoolNonblocking(t *testing.T) {
	pool, _ := NewPool(2, WithNonblocking(true))
	defer pool.Release()

	// Fill the pool with blocking tasks
	var wg sync.WaitGroup
	wg.Add(2)

	for i := 0; i < 10; i++ {
		pool.Submit(func() {
			time.Sleep(100 * time.Millisecond)
			wg.Done()
		})
	}

	// This should fail quickly in nonblocking mode
	err := pool.Submit(func() {})
	if err != ErrPoolOverload {
		t.Errorf("Expected ErrPoolOverload, got %v", err)
	}

	wg.Wait()
}

func TestPoolPanicRecovery(t *testing.T) {
	pool, _ := NewPool(5)
	defer pool.Release()

	var wg sync.WaitGroup
	wg.Add(2)

	// Submit task that panics
	pool.Submit(func() {
		defer wg.Done()
		panic("test panic")
	})

	// Submit normal task - should still work
	pool.Submit(func() {
		defer wg.Done()
	})

	wg.Wait()

	// Pool should still be functional
	if pool.IsClosed() {
		t.Error("Pool should not be closed after panic")
	}
}

func TestPoolClosed(t *testing.T) {
	pool, _ := NewPool(5)
	pool.Release()

	err := pool.Submit(func() {})
	if err != ErrPoolClosed {
		t.Errorf("Expected ErrPoolClosed, got %v", err)
	}

	if !pool.IsClosed() {
		t.Error("Pool should be closed")
	}
}

func TestPoolReboot(t *testing.T) {
	pool, _ := NewPool(5)
	pool.Release()

	if !pool.IsClosed() {
		t.Error("Pool should be closed")
	}

	pool.Reboot()

	if pool.IsClosed() {
		t.Error("Pool should be open after reboot")
	}

	// Should be able to submit tasks
	var wg sync.WaitGroup
	wg.Add(1)
	err := pool.Submit(func() {
		wg.Done()
	})

	if err != nil {
		t.Errorf("Submit after reboot failed: %v", err)
	}

	wg.Wait()
	pool.Release()
}
