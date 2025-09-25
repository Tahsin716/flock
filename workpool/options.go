package workpool

import (
	"runtime"
	"time"
)

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
