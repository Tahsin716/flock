package core

// ErrorMode defines how the Group handles errors from goroutines
type ErrorMode int

const (
	// CollectAll collects all errors and returns them as an aggregate
	CollectAll ErrorMode = iota
	// FailFast cancels the group on first error and returns it
	FailFast
	// IgnoreErrors ignores all errors from goroutines
	IgnoreErrors
)

// Config holds configuration for a Group
type Config struct {
	errorMode   ErrorMode
	errorBuffer int
}

// Option configures a Group
type Option func(*Config)

// BuildConfig creates a config from options, starting with defaults
func BuildConfig(opts []Option) Config {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}
	return config
}

// DefaultConfig returns the default configuration
func DefaultConfig() Config {
	return Config{
		errorMode:   CollectAll,
		errorBuffer: 16,
	}
}

// WithErrorMode sets how errors are handled
func WithErrorMode(mode ErrorMode) Option {
	return func(c *Config) { c.errorMode = mode }
}

// WithErrorBuffer sets the error channel buffer size
func WithErrorBuffer(n int) Option {
	return func(c *Config) {
		if n < 0 {
			n = 0
		}
		c.errorBuffer = n
	}
}
