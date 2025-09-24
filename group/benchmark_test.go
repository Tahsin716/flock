package group

import (
	"context"
	"errors"
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
