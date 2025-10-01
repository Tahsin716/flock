package pool

import "sync/atomic"

// Stats holds pool statistics.
type Stats struct {
	Workers   int64 // Current total number of workers (running + idle)
	Running   int64 // Currently executing jobs
	Idle      int64 // Number of idle workers available
	Submitted int64 // Total submitted jobs
	Completed int64 // Successfully completed jobs
	Failed    int64 // Failed jobs (panicked)
}

// StatsStore provides thread-safe access to statistics.
type StatsStore struct {
	submitted int64
	running   int64
	completed int64
	failed    int64
}

func (s *StatsStore) addSubmitted(n int64) { atomic.AddInt64(&s.submitted, n) }
func (s *StatsStore) addRunning(n int64)   { atomic.AddInt64(&s.running, n) }
func (s *StatsStore) addCompleted(n int64) { atomic.AddInt64(&s.completed, n) }
func (s *StatsStore) addFailed(n int64)    { atomic.AddInt64(&s.failed, n) }

func (s *StatsStore) get() Stats {
	return Stats{
		Submitted: atomic.LoadInt64(&s.submitted),
		Running:   atomic.LoadInt64(&s.running),
		Completed: atomic.LoadInt64(&s.completed),
		Failed:    atomic.LoadInt64(&s.failed),
	}
}
