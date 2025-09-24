package group

import (
	"testing"
)

func BenchmarkGroupCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		g := New()
		_ = g
	}
}
