package pool

import (
	"runtime"
	"time"
)

// Option configures a Pool.
type Option func(*Config)

// Config holds all configuration options for a dynamic pool.
type Config struct {
	MinWorkers   int           // Minimum number of workers to keep alive.
	MaxWorkers   int           // Maximum number of workers the pool can scale to.
	ResultBuffer int           // The size of the buffered results channel.
	MaxIdleTime  time.Duration // Time a worker can be idle before self-terminating.
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() Config {
	gomaxprocs := runtime.GOMAXPROCS(0)
	return Config{
		MinWorkers:   gomaxprocs,
		MaxWorkers:   gomaxprocs * 4,
		ResultBuffer: 100,
		MaxIdleTime:  10 * time.Second,
	}
}

// WithMinWorkers sets the minimum number of workers.
func WithMinWorkers(min int) Option {
	return func(c *Config) {
		if min > 0 {
			c.MinWorkers = min
		}
	}
}

// WithMaxWorkers sets the maximum number of workers.
func WithMaxWorkers(max int) Option {
	return func(c *Config) {
		if max > 0 {
			c.MaxWorkers = max
		}
	}
}

// WithResultBuffer sets the buffer size for the results channel.
func WithResultBuffer(size int) Option {
	return func(c *Config) {
		if size > 0 {
			c.ResultBuffer = size
		}
	}
}

// WithMaxIdleTime sets the duration a worker can be idle before exiting.
func WithMaxIdleTime(t time.Duration) Option {
	return func(c *Config) {
		if t > 0 {
			c.MaxIdleTime = t
		}
	}
}
