package flock

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// workerState represents the current state of a worker
type workerState int32

const (
	StateRunning workerState = iota
	StateSpinning
	StateParked
	StateShutdown
)

type parkingState int32

const (
	parkedState parkingState = iota
	runningState
)

func (s workerState) string() string {
	switch s {
	case StateRunning:
		return "RUNNING"
	case StateSpinning:
		return "SPINNING"
	case StateParked:
		return "PARKED"
	case StateShutdown:
		return "SHUTDOWN"
	default:
		return "UNKNOWN"
	}
}

// worker represents a single worker goroutine
type worker struct {
	id   int
	pool *Pool

	// Single MPSC queue for local tasks
	queue *lockFreeQueue

	// State management
	state atomic.Value // workerState

	// Metrics
	tasksExecuted uint64 // atomic
	tasksFailed   uint64 // atomic
	lastActive    int64  // atomic: unix nano timestamp

	// Parking mechanism
	parkMutex sync.Mutex
	parkCond  *sync.Cond
	parked    uint32 // atomic: 0 = running, 1 = parked
}

// newWorker creates a new worker
func newWorker(id int, pool *Pool, queueSize int) *worker {
	w := &worker{
		id:         id,
		pool:       pool,
		queue:      newQueue(queueSize),
		lastActive: time.Now().UnixNano(),
	}
	w.parkCond = sync.NewCond(&w.parkMutex)
	w.state.Store(StateRunning)
	return w
}

// run is the main worker loop
func (w *worker) run() {
	if w.pool.config.PinWorkerThreads {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
	}

	if w.pool.config.OnWorkerStart != nil {
		w.pool.config.OnWorkerStart(w.id)
	}

	for {
		// Check shutdown
		state := w.pool.state.Load().(PoolState)
		if state == poolStateStopped {
			break
		}

		// Find and execute task
		task := w.findTask()

		if task == nil {
			// Check shutdown again
			state := w.pool.state.Load().(PoolState)
			if state == poolStateStopped {
				break
			}

			// During draining, exit if all queues empty
			if state == poolStateDraining && w.allQueuesEmpty() {
				break
			}

			continue
		}

		// Update last active
		atomic.StoreInt64(&w.lastActive, time.Now().UnixNano())

		// Execute the task
		w.executeTask(task)
	}

	// Cleanup: drain remaining tasks
	w.drainQueues()

	if w.pool.config.OnWorkerStop != nil {
		w.pool.config.OnWorkerStop(w.id)
	}

	w.state.Store(StateShutdown)
}

// findTask attempts to find a task using priority-based search
func (w *worker) findTask() func() {
	// Priority 1: Local queue (FIFO, no contention, fastest)
	if task := w.queue.pop(); task != nil {
		return task
	}

	// Priority 2: Global queue (optimized Go channel, good under contention)
	select {
	case task, ok := <-w.pool.globalQueue:
		if !ok {
			// Global queue closed (shutdown)
			return nil
		}
		return task
	default:
		// Global queue empty, continue to parking
	}

	// Priority 3: Park and wait
	return w.parkAndWait()
}

// parkAndWait parks the worker until work is available
func (w *worker) parkAndWait() func() {
	// Phase 1: Active spinning
	if task := w.spinForWork(); task != nil {
		return task
	}

	// Phase 2: Park with timeout
	return w.parkWithTimeout()
}

// spinForWork attempts to find work through active spinning
func (w *worker) spinForWork() func() {
	w.state.Store(StateSpinning)

	for i := 0; i < w.pool.config.SpinCount; i++ {
		// Check local queue
		if task := w.queue.pop(); task != nil {
			w.state.Store(StateRunning)
			return task
		}

		// Check global queue
		select {
		case task, ok := <-w.pool.globalQueue:
			if !ok {
				return nil // Closed
			}
			w.state.Store(StateRunning)
			return task
		default:
		}

		runtime.Gosched()
	}

	return nil
}

// parkWithTimeout parks the worker and waits for work or timeout
func (w *worker) parkWithTimeout() func() {
	w.parkMutex.Lock()
	defer w.parkMutex.Unlock()

	// Set parked state AFTER acquiring lock
	w.state.Store(StateParked)
	atomic.StoreUint32(&w.parked, uint32(parkedState))

	// Ensure we reset state even on early return
	defer func() {
		atomic.StoreUint32(&w.parked, uint32(runningState))
		w.state.Store(StateRunning)
	}()

	// Double-check for work while holding lock
	if task := w.queue.pop(); task != nil {
		return task
	}

	// Check global queue
	select {
	case task, ok := <-w.pool.globalQueue:
		if !ok {
			return nil // Closed
		}
		return task
	default:
	}

	// Wait for signal or timeout
	w.waitForSignal()

	// Check for work after waking
	if task := w.queue.pop(); task != nil {
		return task
	}

	// Check global queue
	select {
	case task, ok := <-w.pool.globalQueue:
		if !ok {
			return nil // Closed
		}
		return task
	default:
		return nil
	}
}

// waitForSignal waits for a signal or timeout using a timer
func (w *worker) waitForSignal() {
	timer := time.AfterFunc(w.pool.config.MaxParkTime, func() {
		w.parkMutex.Lock()
		w.parkCond.Signal()
		w.parkMutex.Unlock()
	})
	defer timer.Stop()

	w.parkCond.Wait()
}

// signal wakes up a parked worker
func (w *worker) signal() {
	// Fast path: only signal if actually parked
	if atomic.LoadUint32(&w.parked) == uint32(parkedState) {
		w.parkMutex.Lock()
		w.parkCond.Signal()
		w.parkMutex.Unlock()
	}
}

// executeTask executes a task with panic recovery and latency tracking
func (w *worker) executeTask(task func()) {
	startTime := time.Now()

	defer func() {
		if r := recover(); r != nil {
			atomic.AddUint64(&w.tasksFailed, 1)
			if w.pool.config.PanicHandler != nil {
				w.pool.config.PanicHandler(r)
			} else {
				stackTrace := debug.Stack()
				wrappedErr := errWorker(w.id, fmt.Errorf("%v\n%s", r, stackTrace))
				fmt.Printf("[flock error] %v\n", wrappedErr)
			}
		}

		// Always track metrics
		duration := time.Since(startTime)
		w.pool.recordLatency(duration)
		atomic.AddUint64(&w.tasksExecuted, 1)

		// Use sharded metrics
		shardIdx := fastrand() % numMetricShards
		atomic.AddUint64(&w.pool.metricsShards[shardIdx].completed, 1)
		w.pool.submitWg.Done()
	}()

	task()
}

// allQueuesEmpty checks if both local and global queues are empty
func (w *worker) allQueuesEmpty() bool {
	if !w.queue.isEmpty() {
		return false
	}

	// If the global queue is closed and empty, we're done
	if len(w.pool.globalQueue) == 0 {
		select {
		case <-w.pool.globalQueue:
			// Channel closed and drained completely
			return true
		default:
			return true
		}
	}

	return false
}

func (w *worker) drainQueues() {
	// Drain local queue
	for {
		task := w.queue.pop()
		if task == nil {
			break
		}
		shardIdx := fastrand() % numMetricShards
		atomic.AddUint64(&w.pool.metricsShards[shardIdx].dropped, 1)
		w.pool.submitWg.Done()
	}

	// Only worker 0 drains the global queue to avoid contention
	if w.id == 0 {
		for {
			select {
			case _, ok := <-w.pool.globalQueue:
				if !ok {
					return // channel closed — done draining
				}
				// Always Done() for every task
				shardIdx := fastrand() % numMetricShards
				atomic.AddUint64(&w.pool.metricsShards[shardIdx].dropped, 1)
				w.pool.submitWg.Done()
			default:
				return
			}
		}
	}
}

// getState returns the current worker state
func (w *worker) getState() workerState {
	return w.state.Load().(workerState)
}
