package flock

import (
	"context"
	"math/rand"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// INSTANT TASK BENCHMARKS (Queue overhead measurement)
// ============================================================================

func BenchmarkThroughput_Instant_SingleProducer_Flock(b *testing.B) {
	pool, err := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(1024),
	)

	if err != nil {
		b.Fatalf("Failed to create pool: %v", err)
	}

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
	pool, err := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(512),
	)

	if err != nil {
		b.Fatalf("Failed to create pool: %v", err)
	}

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
// I/O-BOUND TASK BENCHMARKS
// ============================================================================

func BenchmarkThroughput_1us_Flock(b *testing.B) {
	pool, err := NewPool(
		WithNumWorkers(runtime.NumCPU()*4),
		WithQueueSizePerWorker(512),
	)

	if err != nil {
		b.Fatalf("Failed to create pool: %v", err)
	}

	defer pool.Shutdown(true)

	numProducers := runtime.NumCPU()
	tasksPerProducer := b.N / numProducers

	b.ResetTimer()
	var wg sync.WaitGroup
	start := time.Now()
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

func BenchmarkThroughput_10us_Flock(b *testing.B) {
	// Workers = NumCPU × (10us / 1us) = 8 × 10 = 80 workers
	pool, err := NewPool(
		WithNumWorkers(runtime.NumCPU()*10),
		WithQueueSizePerWorker(1024),
	)

	if err != nil {
		b.Fatalf("Failed to create pool: %v", err)
	}

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

func BenchmarkThroughput_100us_Flock(b *testing.B) {
	pool, err := NewPool(
		WithNumWorkers(runtime.NumCPU()*100),
		WithQueueSizePerWorker(2048),
	)

	if err != nil {
		b.Fatalf("Failed to create pool: %v", err)
	}

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
	pool, err := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(512),
	)

	if err != nil {
		b.Fatalf("Failed to create pool: %v", err)
	}

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
	pool, err := NewPool()
	defer pool.Shutdown(true)

	if err != nil {
		b.Fatalf("Failed to create pool: %v", err)
	}

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
	pool, err := NewPool(
		WithNumWorkers(runtime.NumCPU()*8),
		WithQueueSizePerWorker(1024),
	)

	if err != nil {
		b.Fatalf("Failed to create pool: %v", err)
	}

	defer pool.Shutdown(true)

	numProducers := runtime.NumCPU()
	tasksPerProducer := b.N / numProducers

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			r := rand.New(rand.NewSource(seed))

			for j := 0; j < tasksPerProducer; j++ {
				// Random distribution: 70% fast, 20% medium, 10% slow
				roll := r.Float64()
				var duration time.Duration
				if roll < 0.70 {
					duration = 1 * time.Microsecond
				} else if roll < 0.90 {
					duration = 10 * time.Microsecond
				} else {
					duration = 100 * time.Microsecond
				}

				pool.Submit(func() {
					time.Sleep(duration)
				})
			}
		}(time.Now().UnixNano() + int64(i))
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
		go func(seed int64) {
			defer wg.Done()
			r := rand.New(rand.NewSource(seed))

			for j := 0; j < tasksPerProducer; j++ {
				roll := r.Float64()
				var duration time.Duration
				if roll < 0.70 {
					duration = 1 * time.Microsecond
				} else if roll < 0.90 {
					duration = 10 * time.Microsecond
				} else {
					duration = 100 * time.Microsecond
				}

				wg.Add(1)
				go func(d time.Duration) {
					defer wg.Done()
					time.Sleep(d)
				}(duration)
			}
		}(time.Now().UnixNano() + int64(i))
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
	pool, err := NewPool(
		WithNumWorkers(runtime.NumCPU()*10),
		WithQueueSizePerWorker(1024),
	)

	if err != nil {
		b.Fatalf("Failed to create pool: %v", err)
	}

	defer pool.Shutdown(true)

	const tasksPerBurst = 100
	burstsNeeded := (b.N + tasksPerBurst - 1) / tasksPerBurst

	b.ResetTimer()
	start := time.Now()

	// Submit all bursts, then wait once
	for i := 0; i < burstsNeeded; i++ {
		for j := 0; j < tasksPerBurst; j++ {
			pool.Submit(func() {
				time.Sleep(10 * time.Microsecond)
			})
		}
		time.Sleep(500 * time.Microsecond) // Short pause between bursts
	}
	pool.Wait()
	elapsed := time.Since(start)
	actualTasks := tasksPerBurst * burstsNeeded
	b.ReportMetric(float64(actualTasks)/elapsed.Seconds(), "tasks/s")
}

func BenchmarkThroughput_BurstLoad_Goroutines(b *testing.B) {
	var wg sync.WaitGroup

	b.ResetTimer()
	start := time.Now()

	const tasksPerBurst = 100
	burstsNeeded := (b.N + tasksPerBurst - 1) / tasksPerBurst

	for i := 0; i < burstsNeeded; i++ {
		for j := 0; j < tasksPerBurst; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				time.Sleep(10 * time.Microsecond)
			}()
		}
		time.Sleep(500 * time.Microsecond) // Short pause between bursts
	}

	wg.Wait()
	elapsed := time.Since(start)
	actualTasks := tasksPerBurst * burstsNeeded
	b.ReportMetric(float64(actualTasks)/elapsed.Seconds(), "tasks/s")
}

// ============================================================================
// GC PRESSURE BENCHMARKS
// ============================================================================

func BenchmarkMemory_GCPressure_Flock(b *testing.B) {
	pool, err := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(512),
	)

	if err != nil {
		b.Fatalf("Failed to create pool: %v", err)
	}

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
	pool, err := NewPool(
		WithNumWorkers(runtime.NumCPU()*4),
		WithQueueSizePerWorker(1024),
	)

	if err != nil {
		b.Fatalf("Failed to create pool: %v", err)
	}

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

// ============================================================================
// REALISTIC API SIMULATION BENCHMARKS
// ============================================================================

// API endpoint latency profiles
var apiProfiles = []struct {
	name     string
	duration time.Duration
	weight   float64
}{
	{"cache_hit", 1 * time.Millisecond, 0.50},      // Fast: Redis/cache
	{"db_query", 5 * time.Millisecond, 0.30},       // Medium: Simple DB query
	{"complex_query", 15 * time.Millisecond, 0.12}, // Slow: Complex join
	{"external_api", 50 * time.Millisecond, 0.06},  // Very slow: Third-party API
	{"timeout", 200 * time.Millisecond, 0.02},      // Timeout/retry scenario
}

func selectAPILatency(r *rand.Rand) time.Duration {
	roll := r.Float64()
	cumulative := 0.0
	for _, profile := range apiProfiles {
		cumulative += profile.weight
		if roll < cumulative {
			return profile.duration
		}
	}
	return apiProfiles[len(apiProfiles)-1].duration
}

type apiMetrics struct {
	mu             sync.Mutex
	latencies      []int64
	completed      int64
	failed         int64
	totalQueueTime int64
}

func (m *apiMetrics) recordCompletion(totalTime, queueTime time.Duration) {
	atomic.AddInt64(&m.completed, 1)
	m.mu.Lock()
	m.latencies = append(m.latencies, totalTime.Nanoseconds())
	m.totalQueueTime += queueTime.Nanoseconds()
	m.mu.Unlock()
}

func (m *apiMetrics) report(b *testing.B, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.latencies) == 0 {
		return
	}

	sort.Slice(m.latencies, func(i, j int) bool {
		return m.latencies[i] < m.latencies[j]
	})

	n := len(m.latencies)
	p50 := time.Duration(m.latencies[n*50/100])
	p95 := time.Duration(m.latencies[n*95/100])
	p99 := time.Duration(m.latencies[n*99/100])
	avgQueue := time.Duration(m.totalQueueTime / int64(n))

	throughput := float64(m.completed) / duration.Seconds()

	b.Logf("API Metrics over %v:", duration)
	b.Logf("  Throughput: %.0f req/s", throughput)
	b.Logf("  Completed: %d", m.completed)
	b.Logf("  p50 latency: %v", p50)
	b.Logf("  p95 latency: %v", p95)
	b.Logf("  p99 latency: %v", p99)
	b.Logf("  Avg queue time: %v", avgQueue)

	b.ReportMetric(throughput, "req/s")
	b.ReportMetric(float64(p50.Microseconds()), "p50_us")
	b.ReportMetric(float64(p95.Microseconds()), "p95_us")
	b.ReportMetric(float64(p99.Microseconds()), "p99_us")
	b.ReportMetric(float64(avgQueue.Microseconds()), "avg_queue_us")
}

func BenchmarkRealisticAPI_30s_Flock(b *testing.B) {
	pool, err := NewPool(
		WithNumWorkers(runtime.NumCPU()*100),
		WithQueueSizePerWorker(2048),
		WithBlockingStrategy(BlockWhenQueueFull),
	)

	if err != nil {
		b.Fatalf("Failed to create pool: %v", err)
	}

	defer pool.Shutdown(true)

	duration := 30 * time.Second
	numClients := runtime.NumCPU() * 2

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	metrics := &apiMetrics{
		latencies: make([]int64, 0, 1000000),
	}

	var wg sync.WaitGroup
	wg.Add(numClients)

	b.ResetTimer()
	start := time.Now()

	for i := 0; i < numClients; i++ {
		go func(clientID int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(time.Now().UnixNano() + int64(clientID)))

			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				apiDuration := selectAPILatency(r)

				submitTime := time.Now()
				execStart := make(chan time.Time, 1)
				done := make(chan struct{})

				err := pool.Submit(func() {
					execStart <- time.Now()
					time.Sleep(apiDuration)
					close(done)
				})

				if err != nil {
					atomic.AddInt64(&metrics.failed, 1)
					continue
				}

				execStartTime := <-execStart
				<-done

				totalLatency := time.Since(submitTime)
				queueWait := execStartTime.Sub(submitTime)
				metrics.recordCompletion(totalLatency, queueWait)

				// Realistic inter-request delay (50-150μs)
				time.Sleep(time.Duration(50+r.Intn(100)) * time.Microsecond)
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)
	b.StopTimer()

	metrics.report(b, elapsed)

	stats := pool.Stats()
	b.Logf("Pool utilization: %.2f%%", stats.Utilization)
	b.Logf("Fallback executions: %d", stats.FallbackExecuted)
}

func BenchmarkRealisticAPI_30s_Goroutines(b *testing.B) {

	duration := 30 * time.Second
	numClients := runtime.NumCPU() * 2

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	metrics := &apiMetrics{
		latencies: make([]int64, 0, 1000000),
	}

	var clientWg sync.WaitGroup
	clientWg.Add(numClients)

	b.ResetTimer()
	start := time.Now()

	for i := 0; i < numClients; i++ {
		go func(clientID int) {
			defer clientWg.Done()
			r := rand.New(rand.NewSource(time.Now().UnixNano() + int64(clientID)))

			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				apiDuration := selectAPILatency(r)

				submitTime := time.Now()

				// Acquire semaphore
				queueWait := time.Since(submitTime)

				var taskWg sync.WaitGroup
				taskWg.Add(1)

				go func(d time.Duration) {
					defer taskWg.Done()
					time.Sleep(d)
				}(apiDuration)

				taskWg.Wait()
				totalLatency := time.Since(submitTime)

				metrics.recordCompletion(totalLatency, queueWait)

				time.Sleep(time.Duration(50+r.Intn(100)) * time.Microsecond)
			}
		}(i)
	}

	clientWg.Wait()
	elapsed := time.Since(start)
	b.StopTimer()

	metrics.report(b, elapsed)
}
