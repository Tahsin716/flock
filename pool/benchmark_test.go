package pool

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Benchmark tests

func BenchmarkPoolSubmit(b *testing.B) {
	pool, _ := NewPool(runtime.GOMAXPROCS(0), WithPreAlloc(true))
	defer pool.Release()

	var counter int32

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pool.Submit(func() {
				atomic.AddInt32(&counter, 1)
			})
		}
	})
	b.StopTimer()

	// Wait for completion outside timing
	for pool.Submitted() != pool.Completed() {
		time.Sleep(time.Millisecond)
	}
}

func BenchmarkPoolSubmitNonPreAlloc(b *testing.B) {
	pool, _ := NewPool(runtime.GOMAXPROCS(0))
	defer pool.Release()

	var counter int32

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pool.Submit(func() {
				atomic.AddInt32(&counter, 1)
			})
		}
	})
	b.StopTimer()

	// Wait for completion outside timing
	for pool.Submitted() != pool.Completed() {
		time.Sleep(time.Millisecond)
	}
}

func BenchmarkPoolThroughput(b *testing.B) {
	pool, _ := NewPool(runtime.GOMAXPROCS(0), WithPreAlloc(true))
	defer pool.Release()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Submit(func() {})
	}
	b.StopTimer()

	// Wait for completion
	for pool.Submitted() != pool.Completed() {
		time.Sleep(time.Millisecond)
	}
}

func BenchmarkPoolVsGoroutines(b *testing.B) {
	b.Run("Pool", func(b *testing.B) {
		pool, _ := NewPool(runtime.GOMAXPROCS(0), WithPreAlloc(true))
		defer pool.Release()

		var wg sync.WaitGroup
		wg.Add(b.N)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pool.Submit(func() {
				wg.Done()
			})
		}
		wg.Wait()
	})

	b.Run("Goroutines", func(b *testing.B) {
		var wg sync.WaitGroup
		wg.Add(b.N)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			go func() {
				wg.Done()
			}()
		}
		wg.Wait()
	})
}

func BenchmarkPoolWithFunc(b *testing.B) {
	var counter int32
	pool, _ := NewPoolWithFunc(runtime.GOMAXPROCS(0), func(arg interface{}) {
		atomic.AddInt32(&counter, 1)
	}, WithPreAlloc(true))
	defer pool.Release()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Invoke(i)
	}
	b.StopTimer()

	// Wait for completion
	for pool.Submitted() != pool.Completed() {
		time.Sleep(time.Millisecond)
	}
}

func BenchmarkPoolGeneric(b *testing.B) {
	var counter int32
	pool, _ := NewPoolWithFuncGeneric(runtime.GOMAXPROCS(0), func(val int) {
		atomic.AddInt32(&counter, 1)
	}, WithPreAlloc(true))
	defer pool.Release()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Invoke(i)
	}
	b.StopTimer()

	// Wait for completion
	for pool.Submitted() != pool.Completed() {
		time.Sleep(time.Millisecond)
	}
}

func BenchmarkPoolCPUBound(b *testing.B) {
	pool, _ := NewPool(runtime.GOMAXPROCS(0), WithPreAlloc(true))
	defer pool.Release()

	var wg sync.WaitGroup
	wg.Add(b.N)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Submit(func() {
			// Simulate CPU work
			sum := 0
			for j := 0; j < 1000; j++ {
				sum += j
			}
			_ = sum
			wg.Done()
		})
	}
	wg.Wait()
}

func BenchmarkPoolMemoryAlloc(b *testing.B) {
	pool, _ := NewPool(runtime.GOMAXPROCS(0), WithPreAlloc(true))
	defer pool.Release()

	var wg sync.WaitGroup

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		wg.Add(1)
		pool.Submit(func() {
			wg.Done()
		})
	}
	wg.Wait()
}

func BenchmarkDirectGoroutineMemoryAlloc(b *testing.B) {
	var wg sync.WaitGroup

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			wg.Done()
		}()
	}
	wg.Wait()
}
