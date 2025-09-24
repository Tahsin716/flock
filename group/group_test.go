package group

import (
	"testing"
)

func TestNew(t *testing.T) {
	g := New()
	if g == nil {
		t.Fatal("New() returned nil")
	}

	if g.ctx == nil {
		t.Error("Group context is nil")
	}

	if g.cancel == nil {
		t.Error("Group cancel function is nil")
	}

	// Check default configuration
	if g.config.errorMode != CollectAll {
		t.Errorf("Expected default error mode %v, got %v", CollectAll, g.config.errorMode)
	}
}
