package group

import (
	"context"
	"testing"
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
