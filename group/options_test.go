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

func TestWithErrorMode(t *testing.T) {
	tests := []struct {
		name string
		mode ErrorMode
	}{
		{"CollectAll", CollectAll},
		{"FailFast", FailFast},
		{"IgnoreErrors", IgnoreErrors},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := New(WithErrorMode(tt.mode))
			if g.config.errorMode != tt.mode {
				t.Errorf("Expected error mode %v, got %v", tt.mode, g.config.errorMode)
			}
		})
	}
}

func TestNewWithOptions(t *testing.T) {
	tests := []struct {
		name     string
		opts     []Option
		expected Config
	}{
		{
			name: "no options",
			opts: []Option{},
			expected: Config{
				errorMode: CollectAll,
			},
		},
		{
			name: "fail fast mode",
			opts: []Option{WithErrorMode(FailFast)},
			expected: Config{
				errorMode: FailFast,
			},
		},
		{
			name: "ignore errors mode",
			opts: []Option{WithErrorMode(IgnoreErrors)},
			expected: Config{
				errorMode: IgnoreErrors,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := New(tt.opts...)
			if g.config.errorMode != tt.expected.errorMode {
				t.Errorf("Expected error mode %v, got %v", tt.expected.errorMode, g.config.errorMode)
			}
		})
	}
}
