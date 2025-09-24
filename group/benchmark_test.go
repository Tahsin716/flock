package group

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkGroupCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		g := New()
		_ = g
	}
}

func BenchmarkGroupCreationWithOptions(b *testing.B) {
	for i := 0; i < b.N; i++ {
		g := New(WithErrorMode(FailFast))
		_ = g
	}
}

func BenchmarkBasicExecution(b *testing.B) {
	for i := 0; i < b.N; i++ {
		g := New()

		for j := 0; j < 10; j++ {
			g.Go(func(ctx context.Context) error {
				return nil
			})
		}

		g.Wait()
	}
}

func BenchmarkExecutionWithErrors(b *testing.B) {
	for i := 0; i < b.N; i++ {
		g := New(WithErrorMode(CollectAll))

		for j := 0; j < 10; j++ {
			j := j
			g.Go(func(ctx context.Context) error {
				if j%2 == 0 {
					return errors.New("benchmark error")
				}
				return nil
			})
		}

		g.Wait()
	}
}

func BenchmarkStatsAccess(b *testing.B) {
	g := New()

	// Add some background goroutines
	for i := 0; i < 100; i++ {
		g.Go(func(ctx context.Context) error {
			time.Sleep(time.Millisecond)
			return nil
		})
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			g.Stats()
		}
	})

	g.Wait()
}

func BenchmarkHighConcurrency(b *testing.B) {
	for i := 0; i < b.N; i++ {
		g := New()

		counter := int32(0)

		for j := 0; j < 1000; j++ {
			g.Go(func(ctx context.Context) error {
				atomic.AddInt32(&counter, 1)
				return nil
			})
		}

		g.Wait()
	}
}

func BenchmarkFailFastMode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		g := New(WithErrorMode(FailFast))

		// First goroutine will fail and cancel others
		g.Go(func(ctx context.Context) error {
			return errors.New("fail fast error")
		})

		// Add many more goroutines that should be cancelled
		for j := 0; j < 100; j++ {
			g.Go(func(ctx context.Context) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Second):
					return nil
				}
			})
		}

		g.Wait()
	}
}
