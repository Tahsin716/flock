package flock

import (
	"runtime"
	"sync"
	"sync/atomic"
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
// Scalability Benchmarks
// ============================================================================

func BenchmarkScalability_Workers1(b *testing.B) {
	benchmarkWithWorkers(b, 1)
}

func BenchmarkScalability_Workers2(b *testing.B) {
	benchmarkWithWorkers(b, 2)
}

func BenchmarkScalability_Workers4(b *testing.B) {
	benchmarkWithWorkers(b, 4)
}

func BenchmarkScalability_Workers8(b *testing.B) {
	benchmarkWithWorkers(b, 8)
}

func BenchmarkScalability_Workers16(b *testing.B) {
	benchmarkWithWorkers(b, 16)
}

func benchmarkWithWorkers(b *testing.B, numWorkers int) {
	pool, _ := NewPool(
		WithNumWorkers(numWorkers),
		WithQueueSizePerWorker(1024),
	)
	defer pool.Shutdown(true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Submit(func() {})
	}
	pool.Wait()
}

// ============================================================================
// Queue Size Impact Benchmarks
// ============================================================================

func BenchmarkQueueSize_64(b *testing.B) {
	benchmarkWithQueueSize(b, 64)
}

func BenchmarkQueueSize_256(b *testing.B) {
	benchmarkWithQueueSize(b, 256)
}

func BenchmarkQueueSize_1024(b *testing.B) {
	benchmarkWithQueueSize(b, 1024)
}

func BenchmarkQueueSize_4096(b *testing.B) {
	benchmarkWithQueueSize(b, 4096)
}

func benchmarkWithQueueSize(b *testing.B, queueSize int) {
	pool, _ := NewPool(
		WithQueueSizePerWorker(queueSize),
	)
	defer pool.Shutdown(true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Submit(func() {})
	}
	pool.Wait()
}

// ============================================================================
// Blocking Strategy Performance Comparison
// ============================================================================

func BenchmarkStrategy_Block(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(4),
		WithQueueSizePerWorker(256),
		WithBlockingStrategy(BlockWhenQueueFull),
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
// Throughput Under Different Task Durations
// ============================================================================

func BenchmarkThroughput_Instant(b *testing.B) {
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

func BenchmarkThroughput_1us(b *testing.B) {
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

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "tasks/sec")
}

func BenchmarkThroughput_10us(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(1024),
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

func BenchmarkThroughput_100us(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(1024),
	)
	defer pool.Shutdown(true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Submit(func() {
			time.Sleep(100 * time.Microsecond)
		})
	}
	pool.Wait()

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "tasks/sec")
}

// ============================================================================
// Latency Benchmarks
// ============================================================================

func BenchmarkLatency_SubmitToExecution(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(1024),
	)
	defer pool.Shutdown(true)

	var totalLatency int64

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		done := make(chan struct{})

		pool.Submit(func() {
			latency := time.Since(start)
			atomic.AddInt64(&totalLatency, int64(latency))
			close(done)
		})

		<-done
	}

	avgLatency := time.Duration(atomic.LoadInt64(&totalLatency) / int64(b.N))
	b.ReportMetric(float64(avgLatency.Microseconds()), "µs/op")
}

func BenchmarkLatency_EmptyPool(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(1024),
	)
	defer pool.Shutdown(true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		done := make(chan struct{})
		start := time.Now()

		pool.Submit(func() {
			close(done)
		})

		<-done
		_ = time.Since(start)
	}
}

func BenchmarkLatency_BusyPool(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(1024),
	)
	defer pool.Shutdown(true)

	// Pre-load pool with work
	for i := 0; i < 100; i++ {
		pool.Submit(func() {
			time.Sleep(10 * time.Millisecond)
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		done := make(chan struct{})
		start := time.Now()

		pool.Submit(func() {
			close(done)
		})

		<-done
		_ = time.Since(start)
	}
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

func BenchmarkAdvanced_Pool_100kTasks(b *testing.B) {
	for i := 0; i < b.N; i++ {
		pool, _ := NewPool(
			WithNumWorkers(runtime.NumCPU()),
			WithQueueSizePerWorker(2048),
		)

		var completed uint64
		for j := 0; j < 100000; j++ {
			pool.Submit(func() {
				atomic.AddUint64(&completed, 1)
			})
		}

		pool.Wait()
		pool.Shutdown(false)

		if completed != 100000 {
			b.Errorf("Expected 100000, got %d", completed)
		}
	}
}

func BenchmarkAdvanced_Goroutines_100kTasks(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		var completed uint64

		wg.Add(100000)
		for j := 0; j < 100000; j++ {
			go func() {
				defer wg.Done()
				atomic.AddUint64(&completed, 1)
			}()
		}

		wg.Wait()

		if completed != 100000 {
			b.Errorf("Expected 100000, got %d", completed)
		}
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

func BenchmarkAdvanced_Pool_MixedLoad(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(1024),
	)
	defer pool.Shutdown(true)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			i++
			if i%10 == 0 {
				// 10% slow tasks
				pool.Submit(func() {
					time.Sleep(100 * time.Microsecond)
				})
			} else {
				// 90% fast tasks
				pool.Submit(func() {})
			}
		}
	})
	pool.Wait()
}

func BenchmarkAdvanced_Goroutines_MixedLoad(b *testing.B) {
	b.ResetTimer()
	var wg sync.WaitGroup
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			i++
			wg.Add(1)
			if i%10 == 0 {
				// 10% slow tasks
				go func() {
					defer wg.Done()
					time.Sleep(100 * time.Microsecond)
				}()
			} else {
				// 90% fast tasks
				go func() {
					defer wg.Done()
				}()
			}
		}
	})
	wg.Wait()
}
