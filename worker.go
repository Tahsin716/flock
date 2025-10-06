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

// Worker represents a single worker goroutine
type Worker struct {
	id   int
	pool *Pool

	// External task queue: MPSC (for Submit/TrySubmit)
	externalTasks *LockFreeQueue

	// Local work deque: Chase-Lev (for internal tasks & work-stealing)
	localWork *ChaseLevDeque

	// State management
	state atomic.Value // WorkerState

	// Metrics
	tasksExecuted uint64 // atomic
	tasksStolen   uint64 // atomic
	lastActive    int64  // atomic: unix nano timestamp

	// Stealing metadata
	seed uint32 // XorShift PRNG seed

	// Parking mechanism
	parkMutex sync.Mutex
	parkCond  *sync.Cond
	wakeup    uint32 // atomic: 1 = wakeup requested
}

// newWorker creates a new worker
func newWorker(id int, pool *Pool, externalQueueSize int, localQueueSize int64) *Worker {
	w := &Worker{
		id:            id,
		pool:          pool,
		externalTasks: New(externalQueueSize),
		localWork:     NewChaseLevDeque(localQueueSize),
		seed:          uint32(time.Now().UnixNano() + int64(id)*1000),
		lastActive:    time.Now().UnixNano(),
	}
	w.parkCond = sync.NewCond(&w.parkMutex)
	w.state.Store(StateRunning)
	return w
}

// run is the main worker loop
func (w *Worker) run() {
	if w.pool.config.PinWorkerThreads {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
	}

	if w.pool.config.OnWorkerStart != nil {
		w.pool.config.OnWorkerStart(w.id)
	}

	idleIterations := 0

	for {
		// Find and execute task
		task := w.findTask()

		if task == nil {
			// Check shutdown
			state := w.pool.state.Load().(PoolState)
			if state == PoolStateStopped {
				break
			}

			idleIterations++

			// Periodic memory cleanup
			if idleIterations%10000 == 0 {
				// Only reset if both queues are empty and idle for 5s
				if w.localWork.IsEmpty() && w.externalTasks.IsEmpty() {
					lastActive := time.Unix(0, atomic.LoadInt64(&w.lastActive))
					if time.Since(lastActive) > 5*time.Second {
						if w.localWork.Capacity() > w.localWork.MinCapacity()*2 {
							w.localWork.Reset()
						}
					}
				}
			}

			continue
		}

		// Reset idle counter and update last active
		idleIterations = 0
		atomic.StoreInt64(&w.lastActive, time.Now().UnixNano())

		// Execute the task
		w.executeTask(task)
	}

	// Final drain during shutdown
	w.drainQueue()

	if w.pool.config.OnWorkerStop != nil {
		w.pool.config.OnWorkerStop(w.id)
	}
}

// findTask attempts to find a task using priority-based search
func (w *Worker) findTask() func() {
	// Priority 1: Promote external tasks to local work (batch)
	w.drainMPSCtoLocal()

	// Priority 2: Local work (LIFO - most recent task first)
	if task := w.localWork.Pop(); task != nil {
		return task
	}

	// Priority 3: Try stealing from others (FIFO - oldest work)
	if task := w.tryStealWithBackoff(); task != nil {
		return task
	}

	// Priority 4: Final check for new external tasks
	w.drainMPSCtoLocal()
	if task := w.localWork.Pop(); task != nil {
		return task
	}

	// Priority 5: Park and wait
	return w.parkAndWait()
}

// drainMPSCtoLocal moves tasks from MPSC to local Chase-Lev deque
// Called by owner only → safe to Push
func (w *Worker) drainMPSCtoLocal() {
	const batchSize = 8 // Tune based on workload
	for i := 0; i < batchSize; i++ {
		task := w.externalTasks.Pop()
		if task == nil {
			break
		}
		w.localWork.Push(task) // SAFE: only owner calls this
	}
}

// tryStealWithBackoff attempts to steal work with exponential backoff
func (w *Worker) tryStealWithBackoff() func() {
	const maxStealAttempts = 256
	numWorkers := len(w.pool.workers)

	if numWorkers <= 1 {
		return nil
	}

	attempts := 0
	consecutiveFailures := 0

	for attempts < maxStealAttempts {
		victimID := w.randomVictim()
		if victimID == w.id {
			attempts++
			continue
		}

		victim := w.pool.workers[victimID]
		stolenTask := victim.localWork.Steal()

		if stolenTask != nil {
			atomic.AddUint64(&w.tasksStolen, 1)
			atomic.AddUint64(&w.pool.metrics.stolen, 1)
			w.tryBatchSteal(victimID, 7)
			return stolenTask
		}

		attempts++
		consecutiveFailures++

		// Exponential backoff
		if consecutiveFailures > 4 {
			backoff := min(consecutiveFailures*2, 64)
			for i := 0; i < backoff; i++ {
				runtime.Gosched()
			}
		}

		// Periodically check own queues
		if attempts%16 == 0 {
			w.drainMPSCtoLocal()
			if task := w.localWork.Pop(); task != nil {
				return task
			}
		}
	}

	return nil
}

// tryBatchSteal attempts to steal multiple tasks at once
func (w *Worker) tryBatchSteal(victimID int, count int) {
	victim := w.pool.workers[victimID]

	for i := 0; i < count; i++ {
		task := victim.localWork.Steal()
		if task == nil {
			break
		}
		w.localWork.Push(task) // ✅ Owner pushing stolen tasks
		atomic.AddUint64(&w.tasksStolen, 1)
		atomic.AddUint64(&w.pool.metrics.stolen, 1)
	}
}

// randomVictim selects a random worker using XorShift PRNG
func (w *Worker) randomVictim() int {
	w.seed ^= w.seed << 13
	w.seed ^= w.seed >> 17
	w.seed ^= w.seed << 5
	return int(w.seed) % len(w.pool.workers)
}

// parkAndWait parks the worker until work is available
func (w *Worker) parkAndWait() func() {
	spinCount := w.pool.config.SpinCount

	// Phase 1: Active spinning
	w.state.Store(StateSpinning)
	for i := 0; i < spinCount; i++ {
		w.drainMPSCtoLocal()
		if task := w.localWork.Pop(); task != nil {
			w.state.Store(StateRunning)
			return task
		}
		runtime.Gosched()
	}

	// Phase 2: Park with proper locking
	w.state.Store(StateParked)
	w.parkMutex.Lock()

	// Double-check: drain and check local work
	w.drainMPSCtoLocal()
	if task := w.localWork.Pop(); task != nil {
		w.parkMutex.Unlock()
		w.state.Store(StateRunning)
		return task
	}

	// Reset wakeup flag
	atomic.StoreUint32(&w.wakeup, 0)

	// Start timeout timer that signals
	timer := time.AfterFunc(w.pool.config.MaxParkTime, func() {
		w.parkMutex.Lock()
		w.parkCond.Signal()
		w.parkMutex.Unlock()
	})

	// Wait (releases lock, blocks, reacquires on signal)
	w.parkCond.Wait()
	w.parkMutex.Unlock()
	timer.Stop() // May be no-op if already fired

	w.state.Store(StateRunning)

	// Final check after waking
	w.drainMPSCtoLocal()
	return w.localWork.Pop()
}

// signal wakes up a parked worker
func (w *Worker) signal() {
	if atomic.SwapUint32(&w.wakeup, 1) == 0 {
		state := w.state.Load().(WorkerState)
		if state == StateParked || state == StateSpinning {
			w.parkMutex.Lock()
			w.parkCond.Signal()
			w.parkMutex.Unlock()
		}
	}
}

// executeTask executes a task with panic recovery
func (w *Worker) executeTask(task func()) {
	defer func() {
		if r := recover(); r != nil {
			if w.pool.config.PanicHandler != nil {
				w.pool.config.PanicHandler(r)
			} else {
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false)
				_ = n // Stack trace in buf[:n]
			}
		}
	}()

	task()

	atomic.AddUint64(&w.tasksExecuted, 1)
	atomic.AddUint64(&w.pool.metrics.completed, 1)
}

// drainQueue processes remaining tasks during shutdown
func (w *Worker) drainQueue() {
	// Drain all external tasks into local work
	for {
		task := w.externalTasks.Pop()
		if task == nil {
			break
		}
		w.localWork.Push(task)
	}

	// Execute all local work
	for {
		task := w.localWork.Pop()
		if task == nil {
			break
		}
		w.executeTask(task)
	}
}

// getState returns the current worker state
func (w *Worker) getState() WorkerState {
	return w.state.Load().(WorkerState)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
