package group

import (
	"strings"
	"testing"
)

func TestPanicError(t *testing.T) {
	panicErr := &PanicError{
		Value: "test panic",
		Stack: "stack trace here",
	}

	errStr := panicErr.Error()
	if !strings.Contains(errStr, "panic: test panic") {
		t.Errorf("PanicError string should contain panic value: %s", errStr)
	}

	if !strings.Contains(errStr, "stack trace here") {
		t.Errorf("PanicError string should contain stack trace: %s", errStr)
	}
}

func TestPanicErrorWithDifferentTypes(t *testing.T) {
	tests := []struct {
		name       string
		panicValue interface{}
	}{
		{"string panic", "string panic"},
		{"int panic", 42},
		{"nil panic", nil},
		{"struct panic", struct{ msg string }{"test"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			panicErr := &PanicError{
				Value: tt.panicValue,
				Stack: "test stack",
			}

			errStr := panicErr.Error()
			if !strings.Contains(errStr, "panic:") {
				t.Errorf("Expected panic prefix in error string: %s", errStr)
			}
		})
	}
}
