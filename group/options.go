package group

// ErrorMode defines how the Group handles errors from goroutines
type ErrorMode int

const (
	// FailFast cancels the group on first error and returns it
	FailFast ErrorMode = iota
	// CollectAll collects all errors and returns them as an aggregate
	CollectAll
	// IgnoreErrors ignores all errors from goroutines
	IgnoreErrors
)

// Config holds configuration for a Group
type Config struct {
	errorMode ErrorMode
}

// Option configures a Group
type Option func(*Config)

// DefaultConfig returns the default configuration
func DefaultConfig() Config {
	return Config{
		errorMode: CollectAll,
	}
}

// WithErrorMode sets how errors are handled
func WithErrorMode(mode ErrorMode) Option {
	return func(c *Config) {
		c.errorMode = mode
	}
}
