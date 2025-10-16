package flock

import (
	"math/rand"
	"runtime"
	"sync"
	"testing"
	"time"
)

// ============================================================================
// Throughput Under Different Task Durations
// ============================================================================

func BenchmarkThroughput_Pool_Instant(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(1024),
	)
	defer pool.Shutdown(true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Submit(func() {
			// Instant task
		})
	}
	pool.Wait()

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "tasks/sec")
}

func BenchmarkThroughput_Goroutines_Instant(b *testing.B) {
	b.ResetTimer()
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			// Instant task
			wg.Done()
		}()
	}
	wg.Wait()

	b.ReportMetric(float64(b.N)/time.Since(start).Seconds(), "tasks/sec")
}

func BenchmarkThroughput_Pool_1us(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*2),
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

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "tasks/sec")
}

func BenchmarkThroughput_Goroutines_1us(b *testing.B) {
	b.ResetTimer()
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			time.Sleep(time.Microsecond)
			wg.Done()
		}()
	}
	wg.Wait()

	b.ReportMetric(float64(b.N)/time.Since(start).Seconds(), "tasks/sec")
}

func BenchmarkThroughput_Pool_10us(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*10),
		WithQueueSizePerWorker(512),
	)
	defer pool.Shutdown(true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Submit(func() {
			time.Sleep(10 * time.Microsecond)
		})
	}
	pool.Wait()

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "tasks/sec")
}

func BenchmarkThroughput_Goroutines_10us(b *testing.B) {
	b.ResetTimer()
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			time.Sleep(10 * time.Microsecond)
			wg.Done()
		}()
	}
	wg.Wait()

	b.ReportMetric(float64(b.N)/time.Since(start).Seconds(), "tasks/sec")
}

// ============================================================================
// Pool vs Goroutine CPU Bound tasks
// ============================================================================

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

// ============================================================================
// Pool vs Goroutine Memory Allocation
// ============================================================================

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

// ============================================================================
// Pool vs Goroutine under different loads
// ============================================================================

func Benchmark_Pool_MixedLoad(b *testing.B) {
	slowTask := func() { time.Sleep(100 * time.Microsecond) }
	fastTask := func() {}

	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*10),
		WithQueueSizePerWorker(512),
	)
	defer pool.Shutdown(true)

	b.ResetTimer()
	b.SetParallelism(100)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Random workload pattern: 10% slow, 90% fast
			if rand.Intn(10) == 0 {
				pool.Submit(slowTask)
			} else {
				pool.Submit(fastTask)
			}

			// Small jitter between requests (simulating uneven arrivals)
			if rand.Intn(100) == 0 {
				time.Sleep(50 * time.Microsecond)
			}
		}
	})

	pool.Wait()
}

func Benchmark_Goroutines_MixedLoad(b *testing.B) {
	slowTask := func() { time.Sleep(100 * time.Microsecond) }
	fastTask := func() {}

	var wg sync.WaitGroup
	b.ResetTimer()
	b.SetParallelism(100)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			wg.Add(1)
			if rand.Intn(10) == 0 {
				go func() {
					defer wg.Done()
					slowTask()
				}()
			} else {
				go func() {
					defer wg.Done()
					fastTask()
				}()
			}

			// Random request jitter
			if rand.Intn(100) == 0 {
				time.Sleep(50 * time.Microsecond)
			}
		}
	})

	wg.Wait()
}

func BenchmarkBurst_Pool_SpikeyLoad(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*10),
		WithQueueSizePerWorker(512),
	)
	defer pool.Shutdown(true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate burst: 100 tasks at once
		for j := 0; j < 100; j++ {
			pool.Submit(func() {
				time.Sleep(10 * time.Microsecond)
			})
		}
		pool.Wait()

		// Small gap between bursts
		time.Sleep(100 * time.Microsecond)
	}
}

func BenchmarkBurst_Goroutines_SpikeyLoad(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(100)

		// Simulate burst: 100 goroutines at once
		for j := 0; j < 100; j++ {
			go func() {
				defer wg.Done()
				time.Sleep(10 * time.Microsecond)
			}()
		}
		wg.Wait()

		// Small gap between bursts
		time.Sleep(100 * time.Microsecond)
	}
}

// ============================================================================
// Memory Pressure Benchmarks
// ============================================================================

func BenchmarkMemory_Pool_GCPressure(b *testing.B) {
	pool, _ := NewPool(
		WithQueueSizePerWorker(512),
	)
	defer pool.Shutdown(true)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pool.Submit(func() {
			// Create garbage
			for j := 0; j < 100; j++ {
				_ = make([]byte, 64)
			}
		})
	}
	pool.Wait()
}

func BenchmarkMemory_Goroutines_GCPressure(b *testing.B) {
	var wg sync.WaitGroup

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Create garbage
			for j := 0; j < 100; j++ {
				_ = make([]byte, 64)
			}
		}()
	}
	wg.Wait()
}

// ============================================================================
// Contention Benchmarks
// ============================================================================

func BenchmarkContention_Pool_HighSubmitters(b *testing.B) {
	pool, _ := NewPool(WithNumWorkers(runtime.NumCPU() * 10))
	defer pool.Shutdown(true)

	b.ResetTimer()
	b.SetParallelism(100) // Many submitters
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pool.Submit(func() {
				time.Sleep(time.Microsecond)
			})
		}
	})
	pool.Wait()
}

func BenchmarkContention_Goroutines_HighSubmitters(b *testing.B) {
	var wg sync.WaitGroup

	b.ResetTimer()
	b.SetParallelism(100) // Many submitters
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			wg.Add(1)
			go func() {
				defer wg.Done()
				time.Sleep(time.Microsecond)
			}()
		}
	})
	wg.Wait()
}
