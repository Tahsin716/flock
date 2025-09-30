package pool

import (
	"time"
)

// Options configures the pool.
type Options struct {
	// ExpiryDuration is the interval for cleaning up idle workers.
	ExpiryDuration time.Duration

	// PreAlloc indicates whether to pre-allocate workers.
	PreAlloc bool

	// MaxBlockingTasks limits blocking when submitting tasks.
	// 0 means unlimited blocking.
	MaxBlockingTasks int

	// Nonblocking controls whether Submit blocks when pool is full.
	Nonblocking bool

	// DisablePurge disables automatic cleanup of idle workers.
	DisablePurge bool
}

// DefaultOptions returns sensible defaults.
func DefaultOptions() Options {
	return Options{
		ExpiryDuration: 10 * time.Second,
		PreAlloc:       false,
		Nonblocking:    false,
		DisablePurge:   false,
	}
}

// Option configures a pool.
type Option func(*Options)

// WithExpiryDuration sets the expiry duration for idle workers.
func WithExpiryDuration(d time.Duration) Option {
	return func(o *Options) {
		o.ExpiryDuration = d
	}
}

// WithPreAlloc enables pre-allocation of workers.
func WithPreAlloc(preAlloc bool) Option {
	return func(o *Options) {
		o.PreAlloc = preAlloc
	}
}

// WithNonblocking enables non-blocking mode.
func WithNonblocking(nonblocking bool) Option {
	return func(o *Options) {
		o.Nonblocking = nonblocking
	}
}

// WithDisablePurge disables automatic cleanup.
func WithDisablePurge(disable bool) Option {
	return func(o *Options) {
		o.DisablePurge = disable
	}
}
