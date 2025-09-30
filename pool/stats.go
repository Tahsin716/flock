package pool

// Stats holds pool statistics.
type Stats struct {
	Workers   int64 // Current number of workers
	Running   int64 // Currently executing jobs
	Submitted int64 // Total submitted jobs
	Completed int64 // Successfully completed jobs
	Failed    int64 // Failed jobs (including panics)
	QueueSize int64 // Jobs waiting in queue
}
