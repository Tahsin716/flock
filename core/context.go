package core

import (
	"context"
	"time"
)

// WithTimeout creates a Group with a context that times out after duration.
func WithTimeout(d time.Duration, opts ...Option) *Group {
	options := DefaultOptions()
	for _, o := range opts {
		o(&options)
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	return &Group{
		ctx:    ctx,
		cancel: cancel,
		errs:   make(chan error, options.bufferSize),
		opts:   options,
	}
}

// WithDeadline creates a Group with a context that expires at deadline.
func WithDeadline(t time.Time, opts ...Option) *Group {
	options := DefaultOptions()
	for _, o := range opts {
		o(&options)
	}
	ctx, cancel := context.WithDeadline(context.Background(), t)
	return &Group{
		ctx:    ctx,
		cancel: cancel,
		errs:   make(chan error, options.bufferSize),
		opts:   options,
	}
}
