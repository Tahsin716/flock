package pool

import "time"

// worker is the interface for workers.
type worker interface {
	run()
	finish()
	lastUsedTime() time.Time
}

// goWorker is a worker that executes tasks.
type goWorker struct {
	pool     *Pool
	task     chan Task
	lastUsed time.Time
}

// workerStack is a LIFO stack of workers for better cache locality.
type workerStack struct {
	items  []worker
	expiry []worker
	size   int32
}
