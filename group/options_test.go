package group

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.errorMode != CollectAll {
		t.Errorf("Expected default error mode %v, got %v", CollectAll, config.errorMode)
	}
}
