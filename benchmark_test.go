package flock

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

// ============================================================================
// Performance Benchmarks
// ============================================================================

func BenchmarkPool_Submit(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(1024),
	)
	defer pool.Shutdown(true)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pool.Submit(func() {})
		}
	})
}

func BenchmarkPool_ExecuteFast(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(1024),
	)
	defer pool.Shutdown(true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Submit(func() {
			// Fast task
		})
	}
	pool.Wait()
}

func BenchmarkPool_ExecuteSlow(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(1024),
	)
	defer pool.Shutdown(true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Submit(func() {
			time.Sleep(time.Microsecond)
		})
	}
	pool.Wait()
}

func BenchmarkPool_HighContention(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(256),
	)
	defer pool.Shutdown(true)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pool.Submit(func() {
				time.Sleep(time.Microsecond)
			})
		}
	})
	pool.Wait()
}

// ============================================================================
// Comparison: Pool vs Raw Goroutines
// ============================================================================

func BenchmarkComparison_Pool_1000Tasks(b *testing.B) {
	for i := 0; i < b.N; i++ {
		pool, _ := NewPool(
			WithNumWorkers(runtime.NumCPU()),
			WithQueueSizePerWorker(256),
		)

		for j := 0; j < 1000; j++ {
			pool.Submit(func() {
				time.Sleep(time.Microsecond)
			})
		}

		pool.Wait()
		pool.Shutdown(false)
	}
}

func BenchmarkComparison_Goroutines_1000Tasks(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(1000)

		for j := 0; j < 1000; j++ {
			go func() {
				defer wg.Done()
				time.Sleep(time.Microsecond)
			}()
		}

		wg.Wait()
	}
}

func BenchmarkComparison_Pool_10000Tasks(b *testing.B) {
	for i := 0; i < b.N; i++ {
		pool, _ := NewPool(
			WithNumWorkers(runtime.NumCPU()),
			WithQueueSizePerWorker(1024),
		)

		for j := 0; j < 10000; j++ {
			pool.Submit(func() {
				time.Sleep(time.Microsecond)
			})
		}

		pool.Wait()
		pool.Shutdown(false)
	}
}

func BenchmarkComparison_Goroutines_10000Tasks(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(10000)

		for j := 0; j < 10000; j++ {
			go func() {
				defer wg.Done()
				time.Sleep(time.Microsecond)
			}()
		}

		wg.Wait()
	}
}

func BenchmarkComparison_Pool_CPUBound(b *testing.B) {
	pool, _ := NewPool()
	defer pool.Shutdown(true)

	cpuWork := func() {
		sum := 0
		for i := 0; i < 1000; i++ {
			sum += i
		}
		_ = sum
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Submit(cpuWork)
	}
	pool.Wait()
}

func BenchmarkComparison_Goroutines_CPUBound(b *testing.B) {
	cpuWork := func() {
		sum := 0
		for i := 0; i < 1000; i++ {
			sum += i
		}
		_ = sum
	}

	b.ResetTimer()
	var wg sync.WaitGroup
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cpuWork()
		}()
	}
	wg.Wait()
}

// Memory allocation benchmarks
func BenchmarkComparison_Pool_MemoryAlloc(b *testing.B) {
	pool, _ := NewPool()
	defer pool.Shutdown(true)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pool.Submit(func() {})
	}
	pool.Wait()
}

func BenchmarkComparison_Goroutines_MemoryAlloc(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	var wg sync.WaitGroup
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
		}()
	}
	wg.Wait()
}
