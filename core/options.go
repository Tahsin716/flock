package core

type ErrorMode int

const (
	FailFast ErrorMode = iota
	CollectAll
	IgnoreErrors
)

type groupOptions struct {
	errorMode  ErrorMode
	bufferSize int
}

func DefaultOptions() groupOptions {
	return groupOptions{
		errorMode:  CollectAll,
		bufferSize: 0,
	}
}

// Option type for functional options.
type Option func(*groupOptions)

func WithFailFast() Option {
	return func(o *groupOptions) { o.errorMode = FailFast }
}

func WithCollectAll() Option {
	return func(o *groupOptions) { o.errorMode = CollectAll }
}

func WithIgnoreErrors() Option {
	return func(o *groupOptions) { o.errorMode = IgnoreErrors }
}

func WithBufferSize(n int) Option {
	return func(o *groupOptions) { o.bufferSize = n }
}
