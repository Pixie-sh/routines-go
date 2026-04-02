package routines

// PanicHandler is called when a worker task panics.
type PanicHandler func(recovered interface{}, stack []byte)

// WorkerPoolOption configures a worker pool.
type WorkerPoolOption func(*WorkerPool)

// WithPanicHandler registers a hook invoked for recovered task panics.
func WithPanicHandler(handler PanicHandler) WorkerPoolOption {
	return func(wp *WorkerPool) {
		wp.panicHandler = handler
	}
}

// WithEvents registers an event channel that receives pool and task lifecycle events.
// Events are published non-blocking: if the channel is full, the event is dropped.
// The caller owns the channel and is responsible for draining it.
func WithEvents(ch chan<- Event) WorkerPoolOption {
	return func(wp *WorkerPool) {
		wp.eventCh = ch
	}
}
