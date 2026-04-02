package routines

// EventType identifies the kind of pool or task lifecycle event.
type EventType int

const (
	// EventPoolStarted is emitted when the pool starts processing.
	EventPoolStarted EventType = iota
	// EventPoolPaused is emitted when the pool pauses new submissions.
	EventPoolPaused
	// EventPoolResumed is emitted when the pool resumes accepting submissions.
	EventPoolResumed
	// EventPoolDraining is emitted when the pool begins draining queued work.
	EventPoolDraining
	// EventPoolStopped is emitted when the pool stops.
	EventPoolStopped
	// EventTaskStarted is emitted when a task begins execution on a worker.
	EventTaskStarted
	// EventTaskCompleted is emitted when a task finishes execution.
	EventTaskCompleted
	// EventTaskPanicked is emitted when a task panics during execution.
	EventTaskPanicked
)

// Event represents a lifecycle event from the worker pool.
type Event struct {
	Type  EventType
	Error error
}

// String returns a human-readable name for the event type.
func (t EventType) String() string {
	switch t {
	case EventPoolStarted:
		return "pool.started"
	case EventPoolPaused:
		return "pool.paused"
	case EventPoolResumed:
		return "pool.resumed"
	case EventPoolDraining:
		return "pool.draining"
	case EventPoolStopped:
		return "pool.stopped"
	case EventTaskStarted:
		return "task.started"
	case EventTaskCompleted:
		return "task.completed"
	case EventTaskPanicked:
		return "task.panicked"
	default:
		return "unknown"
	}
}
