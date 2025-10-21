package flock

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

// ============================================================================
// INSTANT TASK BENCHMARKS
// ============================================================================

func BenchmarkThroughput_Instant_SingleProducer_Flock(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(1024),
	)
	defer pool.Shutdown(true)

	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		pool.Submit(func() {})
	}
	pool.Wait()

	elapsed := time.Since(start)
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "tasks/s")
}

func BenchmarkThroughput_Instant_SingleProducer_Goroutines(b *testing.B) {
	var wg sync.WaitGroup

	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
		}()
	}
	wg.Wait()

	elapsed := time.Since(start)
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "tasks/s")
}

func BenchmarkThroughput_Instant_MultipleProducers_Flock(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(512),
	)
	defer pool.Shutdown(true)

	numProducers := runtime.NumCPU()
	tasksPerProducer := b.N / numProducers

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < tasksPerProducer; j++ {
				pool.Submit(func() {})
			}
		}()
	}
	wg.Wait()
	pool.Wait()

	elapsed := time.Since(start)
	actualTasks := numProducers * tasksPerProducer
	b.ReportMetric(float64(actualTasks)/elapsed.Seconds(), "tasks/s")
}

func BenchmarkThroughput_Instant_MultipleProducers_Goroutines(b *testing.B) {
	numProducers := runtime.NumCPU()
	tasksPerProducer := b.N / numProducers

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < tasksPerProducer; j++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
				}()
			}
		}()
	}
	wg.Wait()

	elapsed := time.Since(start)
	actualTasks := numProducers * tasksPerProducer
	b.ReportMetric(float64(actualTasks)/elapsed.Seconds(), "tasks/s")
}

// ============================================================================
// 1 MICROSECOND TASK BENCHMARKS
// ============================================================================

func BenchmarkThroughput_1us_Flock(b *testing.B) {
	// FIX: Use more workers for I/O-bound tasks
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*4),
		WithQueueSizePerWorker(512),
	)
	defer pool.Shutdown(true)

	numProducers := runtime.NumCPU()
	tasksPerProducer := b.N / numProducers

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < tasksPerProducer; j++ {
				pool.Submit(func() {
					time.Sleep(1 * time.Microsecond)
				})
			}
		}()
	}
	wg.Wait()
	pool.Wait()

	elapsed := time.Since(start)
	actualTasks := numProducers * tasksPerProducer
	b.ReportMetric(float64(actualTasks)/elapsed.Seconds(), "tasks/s")
}

func BenchmarkThroughput_1us_Goroutines(b *testing.B) {
	numProducers := runtime.NumCPU()
	tasksPerProducer := b.N / numProducers

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < tasksPerProducer; j++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					time.Sleep(1 * time.Microsecond)
				}()
			}
		}()
	}
	wg.Wait()

	elapsed := time.Since(start)
	actualTasks := numProducers * tasksPerProducer
	b.ReportMetric(float64(actualTasks)/elapsed.Seconds(), "tasks/s")
}

// ============================================================================
// 10 MICROSECOND TASK BENCHMARKS
// ============================================================================

func BenchmarkThroughput_10us_Flock(b *testing.B) {
	// FIX: Use even more workers for longer I/O tasks
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*10),
		WithQueueSizePerWorker(1024),
	)
	defer pool.Shutdown(true)

	numProducers := runtime.NumCPU()
	tasksPerProducer := b.N / numProducers

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < tasksPerProducer; j++ {
				pool.Submit(func() {
					time.Sleep(10 * time.Microsecond)
				})
			}
		}()
	}
	wg.Wait()
	pool.Wait()

	elapsed := time.Since(start)
	actualTasks := numProducers * tasksPerProducer
	b.ReportMetric(float64(actualTasks)/elapsed.Seconds(), "tasks/s")
}

func BenchmarkThroughput_10us_Goroutines(b *testing.B) {
	numProducers := runtime.NumCPU()
	tasksPerProducer := b.N / numProducers

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < tasksPerProducer; j++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					time.Sleep(10 * time.Microsecond)
				}()
			}
		}()
	}
	wg.Wait()

	elapsed := time.Since(start)
	actualTasks := numProducers * tasksPerProducer
	b.ReportMetric(float64(actualTasks)/elapsed.Seconds(), "tasks/s")
}

// ============================================================================
// 100 MICROSECOND TASK BENCHMARKS
// ============================================================================

func BenchmarkThroughput_100us_Flock(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*50),
		WithQueueSizePerWorker(2048),
	)
	defer pool.Shutdown(true)

	numProducers := runtime.NumCPU()
	tasksPerProducer := b.N / numProducers

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < tasksPerProducer; j++ {
				pool.Submit(func() {
					time.Sleep(100 * time.Microsecond)
				})
			}
		}()
	}
	wg.Wait()
	pool.Wait()

	elapsed := time.Since(start)
	actualTasks := numProducers * tasksPerProducer
	b.ReportMetric(float64(actualTasks)/elapsed.Seconds(), "tasks/s")
}

func BenchmarkThroughput_100us_Goroutines(b *testing.B) {
	numProducers := runtime.NumCPU()
	tasksPerProducer := b.N / numProducers

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < tasksPerProducer; j++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					time.Sleep(100 * time.Microsecond)
				}()
			}
		}()
	}
	wg.Wait()

	elapsed := time.Since(start)
	actualTasks := numProducers * tasksPerProducer
	b.ReportMetric(float64(actualTasks)/elapsed.Seconds(), "tasks/s")
}

// ============================================================================
// CPU-BOUND TASK BENCHMARKS
// ============================================================================

func BenchmarkThroughput_CPUBound_Flock(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(512),
	)
	defer pool.Shutdown(true)

	numProducers := runtime.NumCPU()
	tasksPerProducer := b.N / numProducers

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < tasksPerProducer; j++ {
				pool.Submit(func() {
					sum := 0
					for k := 0; k < 1000; k++ {
						sum += k
					}
					_ = sum
				})
			}
		}()
	}
	wg.Wait()
	pool.Wait()

	elapsed := time.Since(start)
	actualTasks := numProducers * tasksPerProducer
	b.ReportMetric(float64(actualTasks)/elapsed.Seconds(), "tasks/s")
}

func BenchmarkThroughput_CPUBound_Goroutines(b *testing.B) {
	numProducers := runtime.NumCPU()
	tasksPerProducer := b.N / numProducers

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < tasksPerProducer; j++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					sum := 0
					for k := 0; k < 1000; k++ {
						sum += k
					}
					_ = sum
				}()
			}
		}()
	}
	wg.Wait()

	elapsed := time.Since(start)
	actualTasks := numProducers * tasksPerProducer
	b.ReportMetric(float64(actualTasks)/elapsed.Seconds(), "tasks/s")
}

// ============================================================================
// MEMORY ALLOCATION BENCHMARKS
// ============================================================================

func BenchmarkMemory_Alloc_Flock(b *testing.B) {
	pool, _ := NewPool()
	defer pool.Shutdown(true)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pool.Submit(func() {})
	}
	pool.Wait()
}

func BenchmarkMemory_Alloc_Goroutines(b *testing.B) {
	var wg sync.WaitGroup

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
		}()
	}
	wg.Wait()
}

// ============================================================================
// MIXED WORKLOAD BENCHMARKS
// ============================================================================

func BenchmarkThroughput_MixedLoad_Flock(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*10),
		WithQueueSizePerWorker(1024),
	)
	defer pool.Shutdown(true)

	numProducers := runtime.NumCPU()
	tasksPerProducer := b.N / numProducers

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < tasksPerProducer; j++ {
				if j%10 == 0 {
					// 10% slow tasks
					pool.Submit(func() {
						time.Sleep(100 * time.Microsecond)
					})
				} else {
					// 90% instant tasks
					pool.Submit(func() {})
				}
			}
		}()
	}
	wg.Wait()
	pool.Wait()

	elapsed := time.Since(start)
	actualTasks := numProducers * tasksPerProducer
	b.ReportMetric(float64(actualTasks)/elapsed.Seconds(), "tasks/s")
}

func BenchmarkThroughput_MixedLoad_Goroutines(b *testing.B) {
	numProducers := runtime.NumCPU()
	tasksPerProducer := b.N / numProducers

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < tasksPerProducer; j++ {
				wg.Add(1)
				if j%10 == 0 {
					go func() {
						defer wg.Done()
						time.Sleep(100 * time.Microsecond)
					}()
				} else {
					go func() {
						defer wg.Done()
					}()
				}
			}
		}()
	}
	wg.Wait()

	elapsed := time.Since(start)
	actualTasks := numProducers * tasksPerProducer
	b.ReportMetric(float64(actualTasks)/elapsed.Seconds(), "tasks/s")
}

// ============================================================================
// BURST LOAD BENCHMARKS
// ============================================================================

func BenchmarkThroughput_BurstLoad_Flock(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*10),
		WithQueueSizePerWorker(1024),
	)
	defer pool.Shutdown(true)

	const tasksPerBurst = 100
	burstsNeeded := (b.N + tasksPerBurst - 1) / tasksPerBurst

	b.ResetTimer()
	start := time.Now()

	for i := 0; i < burstsNeeded; i++ {
		// Submit burst
		for t := 0; t < tasksPerBurst; t++ {
			pool.Submit(func() {
				time.Sleep(10 * time.Microsecond)
			})
		}
		pool.Wait() // Wait between bursts
	}

	elapsed := time.Since(start)
	actualTasks := burstsNeeded * tasksPerBurst
	b.ReportMetric(float64(actualTasks)/elapsed.Seconds(), "tasks/s")
}

func BenchmarkThroughput_BurstLoad_Goroutines(b *testing.B) {
	b.ResetTimer()
	start := time.Now()

	burstsPerIteration := 10
	tasksPerBurst := 100
	totalTasks := 0

	for i := 0; i < b.N; i++ {
		for burst := 0; burst < burstsPerIteration; burst++ {
			var wg sync.WaitGroup
			wg.Add(tasksPerBurst)

			for t := 0; t < tasksPerBurst; t++ {
				go func() {
					defer wg.Done()
					time.Sleep(10 * time.Microsecond)
				}()
			}
			wg.Wait()
			totalTasks += tasksPerBurst
		}
	}

	elapsed := time.Since(start)
	b.ReportMetric(float64(totalTasks)/elapsed.Seconds(), "tasks/s")
}

// ============================================================================
// GC PRESSURE BENCHMARKS
// ============================================================================

func BenchmarkMemory_GCPressure_Flock(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
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

func BenchmarkMemory_GCPressure_Goroutines(b *testing.B) {
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
// HIGH CONTENTION BENCHMARKS
// ============================================================================

func BenchmarkThroughput_HighContention_Flock(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*2),
		WithQueueSizePerWorker(1024),
	)
	defer pool.Shutdown(true)

	b.ResetTimer()
	start := time.Now()

	b.SetParallelism(100) // Many submitters
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pool.Submit(func() {
				time.Sleep(1 * time.Microsecond)
			})
		}
	})
	pool.Wait()

	elapsed := time.Since(start)
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "tasks/s")
}

func BenchmarkThroughput_HighContention_Goroutines(b *testing.B) {
	var wg sync.WaitGroup

	b.ResetTimer()
	start := time.Now()

	b.SetParallelism(100) // Many submitters
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			wg.Add(1)
			go func() {
				defer wg.Done()
				time.Sleep(1 * time.Microsecond)
			}()
		}
	})
	wg.Wait()

	elapsed := time.Since(start)
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "tasks/s")
}
