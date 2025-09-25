package workpool

import (
	"runtime"
	"time"
)

// Option configures a Group
type Option func(*Config)

// Config holds pool configuration options
type Config struct {
	MaxWorkers      int
	ResultBuffer    int
	MaxIdleTime     time.Duration
	CleanupInterval time.Duration
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		MaxWorkers:      runtime.GOMAXPROCS(0) * 2,
		ResultBuffer:    100,
		MaxIdleTime:     10 * time.Second,
		CleanupInterval: 30 * time.Second,
	}
}

// WithMaxWorkers sets the maximum workers
func WithMaxWorkers(worker int) Option {
	return func(c *Config) {
		if worker <= 0 {
			worker = runtime.GOMAXPROCS(0) * 2
		}
		c.MaxWorkers = worker
	}
}

// WithResultBuffer sets the total result buffer
func WithResultBuffer(buffer int) Option {
	return func(c *Config) {
		if buffer <= 0 {
			buffer = 100
		}
		c.ResultBuffer = buffer
	}
}

// WithMaxIdleTime sets the maximum idle time for a goroutine
func WithMaxIdleTime(t time.Duration) Option {
	return func(c *Config) {
		if t <= 0 {
			t = 10 * time.Second
		}
		c.MaxIdleTime = t
	}
}

// WithCleanupInterval sets the time interval purge idle goroutines
func WithCleanupInterval(t time.Duration) Option {
	return func(c *Config) {
		if t <= 0 {
			t = 10 * time.Second
		}
		c.CleanupInterval = t
	}
}
