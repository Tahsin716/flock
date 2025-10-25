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
// LATENCY DISTRIBUTION FUNCTIONS
// ============================================================================

type latencyDist func(*rand.Rand) time.Duration

var (
	// Light skew (80/15/5) - cache-heavy workload
	lightSkewDistribution = func(r *rand.Rand) time.Duration {
		roll := r.Float64()
		if roll < 0.80 {
			return 1 * time.Microsecond
		}
		if roll < 0.95 {
			return 10 * time.Microsecond
		}
		return 50 * time.Microsecond
	}

	// Medium skew (60/25/10/5) - typical web API
	mediumSkewDistribution = func(r *rand.Rand) time.Duration {
		roll := r.Float64()
		if roll < 0.60 {
			return 1 * time.Microsecond
		}
		if roll < 0.85 {
			return 10 * time.Microsecond
		}
		if roll < 0.95 {
			return 100 * time.Microsecond
		}
		return 1 * time.Millisecond
	}

	// Heavy skew (50/30/15/4/1) - diverse workload
	heavySkewDistribution = func(r *rand.Rand) time.Duration {
		roll := r.Float64()
		if roll < 0.50 {
			return 1 * time.Microsecond
		}
		if roll < 0.80 {
			return 10 * time.Microsecond
		}
		if roll < 0.95 {
			return 100 * time.Microsecond
		}
		if roll < 0.99 {
			return 1 * time.Millisecond
		}
		return 10 * time.Millisecond
	}

	// Bimodal (95/5) - cache hit/miss pattern
	bimodalDistribution = func(r *rand.Rand) time.Duration {
		if r.Float64() < 0.95 {
			return 1 * time.Microsecond
		}
		return 100 * time.Microsecond
	}
)

// ============================================================================
// SECTION 1: INSTANT TASKS (Queue Overhead Measurement)
// ============================================================================

func BenchmarkInstant_SingleProducer_RoundRobin(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(1024),
		WithLoadBalancingStrategy(RoundRobin),
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

func BenchmarkInstant_SingleProducer_P2C(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(1024),
		WithLoadBalancingStrategy(PowerOfTwoChoices),
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

func BenchmarkInstant_SingleProducer_Goroutines(b *testing.B) {
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

func BenchmarkInstant_MultiProducer_RoundRobin(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(512),
		WithLoadBalancingStrategy(RoundRobin),
	)
	defer pool.Shutdown(true)

	runBenchmark(b, pool, func(r *rand.Rand) time.Duration { return 0 }, runtime.NumCPU())
}

func BenchmarkInstant_MultiProducer_P2C(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(512),
		WithLoadBalancingStrategy(PowerOfTwoChoices),
	)
	defer pool.Shutdown(true)

	runBenchmark(b, pool, func(r *rand.Rand) time.Duration { return 0 }, runtime.NumCPU())
}

func BenchmarkInstant_MultiProducer_Goroutines(b *testing.B) {
	runBenchmarkGoroutines(b, func(r *rand.Rand) time.Duration { return 0 }, runtime.NumCPU())
}

// ============================================================================
// SECTION 2: UNIFORM I/O TASKS (Fixed Duration)
// ============================================================================

func BenchmarkUniform_1us_RoundRobin(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*4),
		WithQueueSizePerWorker(512),
		WithLoadBalancingStrategy(RoundRobin),
	)
	defer pool.Shutdown(true)

	runBenchmark(b, pool, func(r *rand.Rand) time.Duration {
		return 1 * time.Microsecond
	}, runtime.NumCPU())
}

func BenchmarkUniform_1us_P2C(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*4),
		WithQueueSizePerWorker(512),
		WithLoadBalancingStrategy(PowerOfTwoChoices),
	)
	defer pool.Shutdown(true)

	runBenchmark(b, pool, func(r *rand.Rand) time.Duration {
		return 1 * time.Microsecond
	}, runtime.NumCPU())
}

func BenchmarkUniform_1us_Goroutines(b *testing.B) {
	runBenchmarkGoroutines(b, func(r *rand.Rand) time.Duration {
		return 1 * time.Microsecond
	}, runtime.NumCPU())
}

func BenchmarkUniform_10us_RoundRobin(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*10),
		WithQueueSizePerWorker(1024),
		WithLoadBalancingStrategy(RoundRobin),
	)
	defer pool.Shutdown(true)

	runBenchmark(b, pool, func(r *rand.Rand) time.Duration {
		return 10 * time.Microsecond
	}, runtime.NumCPU())
}

func BenchmarkUniform_10us_P2C(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*10),
		WithQueueSizePerWorker(1024),
		WithLoadBalancingStrategy(PowerOfTwoChoices),
	)
	defer pool.Shutdown(true)

	runBenchmark(b, pool, func(r *rand.Rand) time.Duration {
		return 10 * time.Microsecond
	}, runtime.NumCPU())
}

func BenchmarkUniform_10us_Goroutines(b *testing.B) {
	runBenchmarkGoroutines(b, func(r *rand.Rand) time.Duration {
		return 10 * time.Microsecond
	}, runtime.NumCPU())
}

func BenchmarkUniform_100us_RoundRobin(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*100),
		WithQueueSizePerWorker(2048),
		WithLoadBalancingStrategy(RoundRobin),
	)
	defer pool.Shutdown(true)

	runBenchmark(b, pool, func(r *rand.Rand) time.Duration {
		return 100 * time.Microsecond
	}, runtime.NumCPU())
}

func BenchmarkUniform_100us_P2C(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*100),
		WithQueueSizePerWorker(2048),
		WithLoadBalancingStrategy(PowerOfTwoChoices),
	)
	defer pool.Shutdown(true)

	runBenchmark(b, pool, func(r *rand.Rand) time.Duration {
		return 100 * time.Microsecond
	}, runtime.NumCPU())
}

func BenchmarkUniform_100us_Goroutines(b *testing.B) {
	runBenchmarkGoroutines(b, func(r *rand.Rand) time.Duration {
		return 100 * time.Microsecond
	}, runtime.NumCPU())
}

// ============================================================================
// SECTION 3: CPU-BOUND TASKS
// ============================================================================

func BenchmarkCPUBound_RoundRobin(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(512),
		WithLoadBalancingStrategy(RoundRobin),
	)
	defer pool.Shutdown(true)

	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		pool.Submit(func() {
			sum := 0
			for k := 0; k < 1000; k++ {
				sum += k
			}
			_ = sum
		})
	}
	elapsed := time.Since(start)
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "tasks/s")
	pool.Wait()
}

func BenchmarkCPUBound_P2C(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(512),
		WithLoadBalancingStrategy(PowerOfTwoChoices),
	)
	defer pool.Shutdown(true)

	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		pool.Submit(func() {
			sum := 0
			for k := 0; k < 1000; k++ {
				sum += k
			}
			_ = sum
		})
	}
	elapsed := time.Since(start)
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "tasks/s")
	pool.Wait()
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
// SECTION 4: VARIABLE LOAD (Light Skew)
// ============================================================================

func BenchmarkLightSkew_RoundRobin(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*10),
		WithQueueSizePerWorker(1024),
		WithLoadBalancingStrategy(RoundRobin),
	)
	defer pool.Shutdown(true)

	runBenchmark(b, pool, lightSkewDistribution, runtime.NumCPU())
}

func BenchmarkLightSkew_P2C(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*10),
		WithQueueSizePerWorker(1024),
		WithLoadBalancingStrategy(PowerOfTwoChoices),
	)
	defer pool.Shutdown(true)

	runBenchmark(b, pool, lightSkewDistribution, runtime.NumCPU())
}

func BenchmarkLightSkew_Goroutines(b *testing.B) {
	runBenchmarkGoroutines(b, lightSkewDistribution, runtime.NumCPU())
}

// ============================================================================
// SECTION 5: VARIABLE LOAD (Medium Skew)
// ============================================================================

func BenchmarkMediumSkew_RoundRobin(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*30),
		WithQueueSizePerWorker(1024),
		WithLoadBalancingStrategy(RoundRobin),
	)
	defer pool.Shutdown(true)

	runBenchmark(b, pool, mediumSkewDistribution, runtime.NumCPU())
}

func BenchmarkMediumSkew_P2C(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*30),
		WithQueueSizePerWorker(1024),
		WithLoadBalancingStrategy(PowerOfTwoChoices),
	)
	defer pool.Shutdown(true)

	runBenchmark(b, pool, mediumSkewDistribution, runtime.NumCPU())
}

func BenchmarkMediumSkew_Goroutines(b *testing.B) {
	runBenchmarkGoroutines(b, mediumSkewDistribution, runtime.NumCPU())
}

// ============================================================================
// SECTION 6: VARIABLE LOAD (Heavy Skew)
// ============================================================================

func BenchmarkHeavySkew_RoundRobin(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*50),
		WithQueueSizePerWorker(2048),
		WithLoadBalancingStrategy(RoundRobin),
	)
	defer pool.Shutdown(true)

	runBenchmark(b, pool, heavySkewDistribution, runtime.NumCPU())
}

func BenchmarkHeavySkew_P2C(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*50),
		WithQueueSizePerWorker(2048),
		WithLoadBalancingStrategy(PowerOfTwoChoices),
	)
	defer pool.Shutdown(true)

	runBenchmark(b, pool, heavySkewDistribution, runtime.NumCPU())
}

func BenchmarkHeavySkew_Goroutines(b *testing.B) {
	runBenchmarkGoroutines(b, heavySkewDistribution, runtime.NumCPU())
}

// ============================================================================
// SECTION 7: BIMODAL DISTRIBUTION (Cache Hit/Miss)
// ============================================================================

func BenchmarkBimodal_RoundRobin(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*10),
		WithQueueSizePerWorker(2048),
		WithLoadBalancingStrategy(RoundRobin),
	)
	defer pool.Shutdown(true)

	runBenchmark(b, pool, bimodalDistribution, runtime.NumCPU())
}

func BenchmarkBimodal_P2C(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*10),
		WithQueueSizePerWorker(2048),
		WithLoadBalancingStrategy(PowerOfTwoChoices),
	)
	defer pool.Shutdown(true)

	runBenchmark(b, pool, bimodalDistribution, runtime.NumCPU())
}

func BenchmarkBimodal_Goroutines(b *testing.B) {
	runBenchmarkGoroutines(b, bimodalDistribution, runtime.NumCPU())
}

// ============================================================================
// SECTION 8: HIGH CONTENTION (100 Submitters)
// ============================================================================

func BenchmarkHighContention_MediumSkew_RoundRobin(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*30),
		WithQueueSizePerWorker(1024),
		WithLoadBalancingStrategy(RoundRobin),
	)
	defer pool.Shutdown(true)

	runBenchmark(b, pool, mediumSkewDistribution, 100)
}

func BenchmarkHighContention_MediumSkew_P2C(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*30),
		WithQueueSizePerWorker(1024),
		WithLoadBalancingStrategy(PowerOfTwoChoices),
	)
	defer pool.Shutdown(true)

	runBenchmark(b, pool, mediumSkewDistribution, 100)
}

// ============================================================================
// SECTION 9: PATHOLOGICAL CASE (Worst for Round-Robin)
// ============================================================================

func BenchmarkPathological_RoundRobin(b *testing.B) {
	numWorkers := 8
	pool, _ := NewPool(
		WithNumWorkers(numWorkers),
		WithQueueSizePerWorker(1024),
		WithLoadBalancingStrategy(RoundRobin),
	)
	defer pool.Shutdown(true)

	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		var duration time.Duration
		if i%numWorkers == 0 { // Every 8th task
			duration = 100 * time.Microsecond
		} else {
			duration = 1 * time.Microsecond
		}
		pool.Submit(func() {
			time.Sleep(duration)
		})
	}

	pool.Wait()
	elapsed := time.Since(start)
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "tasks/s")
}

func BenchmarkPathological_P2C(b *testing.B) {
	numWorkers := 8
	pool, _ := NewPool(
		WithNumWorkers(numWorkers),
		WithQueueSizePerWorker(1024),
		WithLoadBalancingStrategy(PowerOfTwoChoices),
	)
	defer pool.Shutdown(true)

	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		var duration time.Duration
		if i%numWorkers == 0 { // Every 8th task
			duration = 100 * time.Microsecond
		} else {
			duration = 1 * time.Microsecond
		}
		pool.Submit(func() {
			time.Sleep(duration)
		})
	}

	pool.Wait()
	elapsed := time.Since(start)
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "tasks/s")
}

// ============================================================================
// SECTION 10: LATENCY BENCHMARKS (p50/p95/p99)
// ============================================================================

func BenchmarkLatency_MediumSkew_RoundRobin(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*30),
		WithQueueSizePerWorker(1024),
		WithLoadBalancingStrategy(RoundRobin),
	)
	defer pool.Shutdown(true)

	runLatencyBenchmark(b, pool, mediumSkewDistribution)
}

func BenchmarkLatency_MediumSkew_P2C(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*30),
		WithQueueSizePerWorker(1024),
		WithLoadBalancingStrategy(PowerOfTwoChoices),
	)
	defer pool.Shutdown(true)

	runLatencyBenchmark(b, pool, mediumSkewDistribution)
}

func BenchmarkLatency_HeavySkew_RoundRobin(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*100),
		WithQueueSizePerWorker(2048),
		WithLoadBalancingStrategy(RoundRobin),
	)
	defer pool.Shutdown(true)

	runLatencyBenchmark(b, pool, heavySkewDistribution)
}

func BenchmarkLatency_HeavySkew_P2C(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*100),
		WithQueueSizePerWorker(2048),
		WithLoadBalancingStrategy(PowerOfTwoChoices),
	)
	defer pool.Shutdown(true)

	runLatencyBenchmark(b, pool, heavySkewDistribution)
}

// ============================================================================
// SECTION 11: MEMORY/GC PRESSURE
// ============================================================================

func BenchmarkMemory_Alloc_RoundRobin(b *testing.B) {
	pool, _ := NewPool(
		WithLoadBalancingStrategy(RoundRobin),
	)
	defer pool.Shutdown(true)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pool.Submit(func() {})
	}
	pool.Wait()
}

func BenchmarkMemory_Alloc_P2C(b *testing.B) {
	pool, _ := NewPool(
		WithLoadBalancingStrategy(PowerOfTwoChoices),
	)
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

func BenchmarkMemory_GCPressure_RoundRobin(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(512),
		WithLoadBalancingStrategy(RoundRobin),
	)
	defer pool.Shutdown(true)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pool.Submit(func() {
			for j := 0; j < 100; j++ {
				_ = make([]byte, 64)
			}
		})
	}
	pool.Wait()
}

func BenchmarkMemory_GCPressure_P2C(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()),
		WithQueueSizePerWorker(512),
		WithLoadBalancingStrategy(PowerOfTwoChoices),
	)
	defer pool.Shutdown(true)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pool.Submit(func() {
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
			for j := 0; j < 100; j++ {
				_ = make([]byte, 64)
			}
		}()
	}
	wg.Wait()
}

// ============================================================================
// SECTION 12: REALISTIC WEB API SIMULATION (30 seconds)
// ============================================================================

var apiProfiles = []struct {
	name     string
	duration time.Duration
	weight   float64
}{
	{"cache_hit", 1 * time.Millisecond, 0.50},
	{"db_query", 5 * time.Millisecond, 0.30},
	{"complex_query", 15 * time.Millisecond, 0.12},
	{"external_api", 50 * time.Millisecond, 0.06},
	{"timeout", 200 * time.Millisecond, 0.02},
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

func BenchmarkRealisticAPI_30s_RoundRobin(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*4),
		WithQueueSizePerWorker(4096),
		WithBlockingStrategy(BlockWhenQueueFull),
		WithLoadBalancingStrategy(RoundRobin),
	)
	defer pool.Shutdown(true)

	runRealisticAPIBenchmark(b, pool, 30*time.Second)
}

func BenchmarkRealisticAPI_30s_P2C(b *testing.B) {
	pool, _ := NewPool(
		WithNumWorkers(runtime.NumCPU()*4),
		WithQueueSizePerWorker(4096),
		WithBlockingStrategy(BlockWhenQueueFull),
		WithLoadBalancingStrategy(PowerOfTwoChoices),
	)
	defer pool.Shutdown(true)

	runRealisticAPIBenchmark(b, pool, 30*time.Second)
}

func BenchmarkRealisticAPI_30s_Goroutines(b *testing.B) {
	runRealisticAPIBenchmarkGoroutines(b, 30*time.Second)
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func runBenchmark(b *testing.B, pool *Pool, dist latencyDist, numProducers int) {
	tasksPerProducer := b.N / numProducers
	actualTasks := numProducers * tasksPerProducer

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			r := rand.New(rand.NewSource(seed))
			for j := 0; j < tasksPerProducer; j++ {
				duration := dist(r)
				pool.Submit(func() {
					if duration > 0 {
						time.Sleep(duration)
					}
				})
			}
		}(time.Now().UnixNano() + int64(i))
	}
	wg.Wait()
	pool.Wait()

	elapsed := time.Since(start)
	b.ReportMetric(float64(actualTasks)/elapsed.Seconds(), "tasks/s")
}

func runBenchmarkGoroutines(b *testing.B, dist latencyDist, numProducers int) {
	tasksPerProducer := b.N / numProducers
	actualTasks := numProducers * tasksPerProducer

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			r := rand.New(rand.NewSource(seed))
			for j := 0; j < tasksPerProducer; j++ {
				duration := dist(r)
				wg.Add(1)
				go func(d time.Duration) {
					defer wg.Done()
					if d > 0 {
						time.Sleep(d)
					}
				}(duration)
			}
		}(time.Now().UnixNano() + int64(i))
	}
	wg.Wait()

	elapsed := time.Since(start)
	b.ReportMetric(float64(actualTasks)/elapsed.Seconds(), "tasks/s")
}

func runLatencyBenchmark(b *testing.B, pool *Pool, dist latencyDist) {
	numProducers := runtime.NumCPU()
	tasksPerProducer := b.N / numProducers

	type producerMetrics struct {
		latencies []time.Duration
	}
	metrics := make([]producerMetrics, numProducers)
	for i := range metrics {
		metrics[i].latencies = make([]time.Duration, 0, tasksPerProducer)
	}

	b.ResetTimer()

	var wg sync.WaitGroup
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func(producerID int, seed int64) {
			defer wg.Done()
			r := rand.New(rand.NewSource(seed))

			for j := 0; j < tasksPerProducer; j++ {
				duration := dist(r)

				start := time.Now()
				done := make(chan struct{})

				pool.Submit(func() {
					time.Sleep(duration)
					close(done)
				})

				<-done
				elapsed := time.Since(start)

				metrics[producerID].latencies = append(metrics[producerID].latencies, elapsed)
			}
		}(i, time.Now().UnixNano()+int64(i))
	}
	wg.Wait()
	pool.Wait()

	var allLatencies []time.Duration
	for i := range metrics {
		allLatencies = append(allLatencies, metrics[i].latencies...)
	}

	sort.Slice(allLatencies, func(i, j int) bool {
		return allLatencies[i] < allLatencies[j]
	})

	if len(allLatencies) > 0 {
		n := len(allLatencies)
		p50 := allLatencies[n*50/100]
		p95 := allLatencies[n*95/100]
		p99 := allLatencies[n*99/100]
		pMax := allLatencies[n-1]

		b.ReportMetric(float64(p50.Microseconds()), "p50_us")
		b.ReportMetric(float64(p95.Microseconds()), "p95_us")
		b.ReportMetric(float64(p99.Microseconds()), "p99_us")
		b.ReportMetric(float64(pMax.Microseconds()), "max_us")
	}
}

func runRealisticAPIBenchmark(b *testing.B, pool *Pool, duration time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	metrics := &apiMetrics{
		latencies: make([]int64, 0, 1000000),
	}
	numClients := runtime.NumCPU()

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

func runRealisticAPIBenchmarkGoroutines(b *testing.B, duration time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	metrics := &apiMetrics{
		latencies: make([]int64, 0, 1000000),
	}

	numClients := runtime.NumCPU()

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
				queueWait := time.Duration(0)

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
