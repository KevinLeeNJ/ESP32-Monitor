package taskmonitor

import "context"

// Adapter observes tasks and projects platform/source details into the common model.
// Run blocks until cancellation or a permanent configuration error. It owns and
// closes only its own resources. The caller starts it once and waits for exit.
// Snapshot must be concurrency-safe, non-blocking on I/O, and return an owned
// copy. Disconnected, stale or unverified evidence must produce Unknown, never
// an inferred Idle/Completed. ActiveCount is nil when coverage is uncertain.
// WaitReady returns after the first observation/error or context cancellation.
// Implementations must never start/resume tasks or answer approvals.
type Adapter interface {
	ID() string
	Run(context.Context)
	WaitReady(context.Context)
	Snapshot() Tasks
}
