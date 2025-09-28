package pool

// Stats holds pool statistics
type Stats struct {
	Running   int64 // Currently running jobs
	Submitted int64 // Total submitted jobs
	Completed int64 // Successfully completed jobs
	Failed    int64 // Failed jobs
}
