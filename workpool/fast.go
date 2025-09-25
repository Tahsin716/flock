package workpool

import "sync"

// FastPool is an ultra-high-performance pool for fire-and-forget functions
// This directly competes with ants.Pool performance
type FastPool struct {
	workers  []*fastWorker
	workerCh chan *fastWorker
	capacity int32

	// Statistics
	submitted int64
	running   int64

	// Lifecycle
	closed int64
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// fastWorker is optimized for minimal overhead
type fastWorker struct {
	taskCh chan func()
	pool   *FastPool
}
