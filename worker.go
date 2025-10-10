package flock

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// WorkerState represents the current state of a worker
type WorkerState int32

const (
	StateRunning WorkerState = iota
	StateSpinning
	StateParked
	StateShutdown
)

type parkingState int32

const (
	parkedState parkingState = iota
	runningState
)

func (s WorkerState) String() string {
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

// worker represents a single worker goroutine with MPSC queue
type worker struct {
	id   int
	pool *Pool

	// Single MPSC queue for all operations
	queue *lockFreeQueue

	// State management
	state atomic.Value // WorkerState

	// Metrics
	tasksExecuted uint64 // atomic
	tasksFailed   uint64 // atomic
	lastActive    int64  // atomic: unix nano timestamp

	// Parking mechanism
	parkMutex sync.Mutex
	parkCond  *sync.Cond
	parked    uint32 // atomic: 0 = running, 1 = parked
}

// newWorker creates a new worker with MPSC queue
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
		// Find and execute task
		task := w.findTask()

		if task == nil {
			// Check shutdown
			state := w.pool.state.Load().(PoolState)
			if state == poolStateStopped {
				break
			}

			// During draining, exit if queue empty
			if state == poolStateDraining && w.queue.isEmpty() {
				break
			}

			continue
		}

		// update last active
		atomic.StoreInt64(&w.lastActive, time.Now().UnixNano())

		// Execute the task
		w.executeTask(task)
	}

	// Cleanup: drain remaining tasks
	w.drainQueue()

	if w.pool.config.OnWorkerStop != nil {
		w.pool.config.OnWorkerStop(w.id)
	}
}

// findTask attempts to find a task using priority-based search
func (w *worker) findTask() func() {
	// Priority 1: Own queue (FIFO - single consumer)
	if task := w.queue.pop(); task != nil {
		return task
	}

	// Priority 2: Park and wait
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
		if task := w.queue.pop(); task != nil {
			w.state.Store(StateRunning)
			return task
		}
		runtime.Gosched()
	}

	return nil
}

// parkWithTimeout parks the worker and waits for work or timeout
func (w *worker) parkWithTimeout() func() {
	w.parkMutex.Lock()
	defer w.parkMutex.Unlock()

	// Set parked state AFTER acquiring lock to prevent lost wakeups
	w.state.Store(StateParked)
	atomic.StoreUint32(&w.parked, uint32(parkedState))

	// Ensure we reset state even on early return
	defer func() {
		atomic.StoreUint32(&w.parked, uint32(runningState))
		w.state.Store(StateRunning)
	}()

	// Double-check for work while holding lock (prevents lost wakeup)
	if task := w.queue.pop(); task != nil {
		return task
	}

	// Wait for signal or timeout
	w.waitForSignal()

	// Check for work after waking
	return w.queue.pop()
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
				// Default: capture stack trace silently
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false)
				_ = n // Stack trace in buf[:n]
			}
		}

		// Always track metrics even if panic occurred
		duration := time.Since(startTime)
		w.pool.recordLatency(duration)
		atomic.AddUint64(&w.tasksExecuted, 1)
		atomic.AddUint64(&w.pool.metrics.completed, 1)
		w.pool.submitWg.Done()
	}()

	// Execute the task
	task()
}

// drainQueue processes remaining tasks during shutdown
func (w *worker) drainQueue() {
	for {
		task := w.queue.pop()
		if task == nil {
			break
		}
		w.executeTask(task)
	}
}

// getState returns the current worker state
func (w *worker) getState() WorkerState {
	return w.state.Load().(WorkerState)
}
