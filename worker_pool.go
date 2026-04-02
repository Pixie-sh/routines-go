package routines

import (
	"context"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/pixie-sh/errors-go"
	logger "github.com/pixie-sh/logger-go/logger"
)

// TaskResult represents the result of a task execution.
type TaskResult struct {
	Result interface{}
	Error  error
}

type taskRequest struct {
	task       Task
	taskCtx    context.Context
	resultChan chan TaskResult
}

type workerPoolState int

const (
	workerPoolStateNew workerPoolState = iota
	workerPoolStateRunning
	workerPoolStatePaused
	workerPoolStateDraining
	workerPoolStateStopped
)

// WorkerPool represents a pool of workers that can process tasks from a queue.
type WorkerPool struct {
	workers    int
	queue      chan *taskRequest
	ctx        context.Context
	cancelFunc context.CancelFunc

	wg     sync.WaitGroup
	taskWg sync.WaitGroup

	mu              sync.Mutex
	state           workerPoolState
	acceptingClosed bool
	acceptingDone   chan struct{}
	panicHandler    PanicHandler
	closedSignal    atomic.Bool
	eventCh         chan<- Event

	errorsMu   sync.Mutex
	taskErrors []error
}

// NewWorkerPool creates a new worker pool with the specified number of workers and queue size.
func NewWorkerPool(ctx context.Context, workers int, queueSize int, opts ...WorkerPoolOption) (*WorkerPool, error) {
	if workers <= 0 {
		return nil, errors.New("number of workers must be greater than 0")
	}
	if queueSize <= 0 {
		return nil, errors.New("queue size must be greater than 0")
	}

	poolCtx, cancelFunc := context.WithCancel(ctx)
	wp := &WorkerPool{
		workers:       workers,
		queue:         make(chan *taskRequest, queueSize),
		ctx:           poolCtx,
		cancelFunc:    cancelFunc,
		state:         workerPoolStateNew,
		acceptingDone: make(chan struct{}),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(wp)
		}
	}

	context.AfterFunc(poolCtx, func() {
		wp.onPoolContextDone()
	})

	return wp, nil
}

// Start starts the worker pool.
func (wp *WorkerPool) Start() error {
	wp.mu.Lock()
	if wp.state != workerPoolStateNew {
		wp.mu.Unlock()
		return errors.New("worker pool already started")
	}

	wp.state = workerPoolStateRunning
	wp.wg.Add(wp.workers)
	ready := make(chan struct{}, wp.workers)
	for i := 0; i < wp.workers; i++ {
		go wp.taskConsumer(ready)
	}

	for i := 0; i < wp.workers; i++ {
		<-ready
	}
	wp.mu.Unlock()

	wp.emit(Event{Type: EventPoolStarted})
	return nil
}

func (wp *WorkerPool) taskConsumer(ready chan<- struct{}) {
	defer wp.wg.Done()
	ready <- struct{}{}

	for {
		select {
		case <-wp.ctx.Done():
			return
		case req, ok := <-wp.queue:
			if !ok {
				return
			}
			wp.executeTask(req)
		}
	}
}

func (wp *WorkerPool) executeTask(req *taskRequest) {
	ctx, cancel := composeTaskContext(wp.ctx, req.taskCtx)
	defer cancel()

	wp.emit(Event{Type: EventTaskStarted})
	result := wp.runTask(req.task, ctx)
	if result.Error != nil {
		wp.emit(Event{Type: EventTaskCompleted, Error: result.Error})
	} else {
		wp.emit(Event{Type: EventTaskCompleted})
	}
	wp.finalizeTask(req, result)
}

func (wp *WorkerPool) runTask(task Task, ctx context.Context) (result TaskResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			stack := debug.Stack()
			wp.handleRecoveredPanic(recovered, stack)
			result = TaskResult{Error: errors.New("task panic: %v", recovered)}
		}
	}()

	value, err := task(ctx)
	return TaskResult{Result: value, Error: err}
}

func (wp *WorkerPool) handleRecoveredPanic(recovered interface{}, stack []byte) {
	logger.Error("worker pool task panic: %v\n%s", recovered, stack)
	wp.emit(Event{Type: EventTaskPanicked, Error: errors.New("task panic: %v", recovered)})

	if wp.panicHandler == nil {
		return
	}

	defer func() {
		if handlerPanic := recover(); handlerPanic != nil {
			logger.Error("worker pool panic handler panic: %v", handlerPanic)
		}
	}()

	wp.panicHandler(recovered, stack)
}

func (wp *WorkerPool) finalizeTask(req *taskRequest, result TaskResult) {
	defer wp.taskWg.Done()

	if result.Error != nil {
		wp.recordTaskError(result.Error)
	}

	if req.resultChan != nil {
		req.resultChan <- result
		close(req.resultChan)
	}
}

func (wp *WorkerPool) recordTaskError(err error) {
	wp.errorsMu.Lock()
	defer wp.errorsMu.Unlock()

	wp.taskErrors = append(wp.taskErrors, err)
}

func (wp *WorkerPool) submitTask(task Task, taskCtx context.Context, submitCtx context.Context, blocking bool) (<-chan TaskResult, error) {
	if task == nil {
		return nil, errors.New("task cannot be nil")
	}

	if submitCtx == nil {
		submitCtx = context.Background()
	}

	req := &taskRequest{
		task:       task,
		taskCtx:    taskCtx,
		resultChan: make(chan TaskResult, 1),
	}

	wp.mu.Lock()
	if err := wp.submitAvailabilityErrorLocked(); err != nil {
		wp.mu.Unlock()
		return nil, err
	}
	wp.taskWg.Add(1)
	acceptingDone := wp.acceptingDone
	wp.mu.Unlock()

	if blocking {
		select {
		case wp.queue <- req:
			return req.resultChan, nil
		case <-submitCtx.Done():
			wp.taskWg.Done()
			return nil, submitCtx.Err()
		case <-wp.ctx.Done():
			wp.taskWg.Done()
			return nil, errors.New("worker pool stopped")
		case <-acceptingDone:
			wp.taskWg.Done()
			return nil, wp.submissionClosedError()
		}
	}

	select {
	case wp.queue <- req:
		return req.resultChan, nil
	case <-wp.ctx.Done():
		wp.taskWg.Done()
		return nil, errors.New("worker pool stopped")
	case <-acceptingDone:
		wp.taskWg.Done()
		return nil, wp.submissionClosedError()
	default:
		wp.taskWg.Done()
		return nil, errors.New("queue is full")
	}
}

func (wp *WorkerPool) submitAvailabilityErrorLocked() error {
	if err := wp.ctx.Err(); err != nil {
		return errors.New("worker pool stopped")
	}

	switch wp.state {
	case workerPoolStatePaused:
		return errors.New("worker pool paused")
	case workerPoolStateDraining:
		return errors.New("worker pool draining")
	case workerPoolStateStopped:
		return errors.New("worker pool stopped")
	default:
		return nil
	}
}

func (wp *WorkerPool) submissionClosedError() error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	switch wp.state {
	case workerPoolStatePaused:
		return errors.New("worker pool paused")
	case workerPoolStateDraining:
		return errors.New("worker pool draining")
	default:
		return errors.New("worker pool stopped")
	}
}

func (wp *WorkerPool) closeAcceptingLocked() {
	if wp.acceptingClosed {
		return
	}

	wp.closedSignal.Store(true)
	close(wp.acceptingDone)
	wp.acceptingClosed = true
}

func (wp *WorkerPool) onPoolContextDone() {
	wp.mu.Lock()
	wp.state = workerPoolStateStopped
	wp.closeAcceptingLocked()
	wp.mu.Unlock()

	wp.rejectQueuedTasks(errors.New("worker pool stopped"))
	wp.emit(Event{Type: EventPoolStopped})
}

func (wp *WorkerPool) rejectQueuedTasks(err error) {
	for {
		select {
		case req, ok := <-wp.queue:
			if !ok {
				return
			}
			wp.finalizeTask(req, TaskResult{Error: err})
		default:
			return
		}
	}
}

// AddTask adds a task to the queue.
// It returns a channel that receives a single task result.
func (wp *WorkerPool) AddTask(task Task) (<-chan TaskResult, error) {
	return wp.submitTask(task, nil, nil, false)
}

// AddTaskWithContext adds a task to the queue with a task-specific context.
// The task context is canceled when either the pool context or task context is canceled.
func (wp *WorkerPool) AddTaskWithContext(taskCtx context.Context, task Task) (<-chan TaskResult, error) {
	return wp.submitTask(task, taskCtx, nil, false)
}

// AddTaskBlocking waits for queue capacity and then adds a task to the queue.
// It returns early if submitCtx or the pool context is canceled.
func (wp *WorkerPool) AddTaskBlocking(submitCtx context.Context, task Task) (<-chan TaskResult, error) {
	return wp.submitTask(task, nil, submitCtx, true)
}

// AddTaskAndWait adds a task to the queue and waits for it to complete.
func (wp *WorkerPool) AddTaskAndWait(task Task) (interface{}, error) {
	resultChan, err := wp.AddTask(task)
	if err != nil {
		return nil, err
	}

	select {
	case result, ok := <-resultChan:
		if !ok {
			return nil, errors.New("worker pool stopped before task completion")
		}
		return result.Result, result.Error
	case <-wp.ctx.Done():
		return nil, errors.New("worker pool stopped before task completion")
	}
}

// Stop stops the worker pool and waits for all workers to finish.
func (wp *WorkerPool) Stop() {
	wp.mu.Lock()
	if wp.state == workerPoolStateStopped {
		wp.mu.Unlock()
		return
	}
	wp.state = workerPoolStateStopped
	wp.closeAcceptingLocked()
	wp.mu.Unlock()

	wp.cancelFunc()
	wp.rejectQueuedTasks(errors.New("worker pool stopped"))
	if wp.closedSignal.CompareAndSwap(false, true) {
		close(wp.queue)
	}
	wp.wg.Wait()
	wp.emit(Event{Type: EventPoolStopped})
}

// Drain stops accepting new tasks, waits for accepted work to finish, and then stops the pool.
func (wp *WorkerPool) Drain() error {
	wp.mu.Lock()
	switch wp.state {
	case workerPoolStateDraining:
		wp.mu.Unlock()
		return errors.New("worker pool draining")
	case workerPoolStateStopped:
		wp.mu.Unlock()
		return errors.New("worker pool stopped")
	default:
		wp.state = workerPoolStateDraining
		wp.closeAcceptingLocked()
	}
	wp.mu.Unlock()

	wp.emit(Event{Type: EventPoolDraining})
	wp.taskWg.Wait()

	wp.mu.Lock()
	wp.state = workerPoolStateStopped
	wp.mu.Unlock()

	wp.cancelFunc()
	if wp.closedSignal.CompareAndSwap(false, true) {
		close(wp.queue)
	}
	wp.wg.Wait()
	wp.emit(Event{Type: EventPoolStopped})

	return nil
}

// Errors returns a snapshot of task execution errors recorded by the pool.
func (wp *WorkerPool) Errors() []error {
	wp.errorsMu.Lock()
	defer wp.errorsMu.Unlock()

	if len(wp.taskErrors) == 0 {
		return nil
	}

	errs := make([]error, len(wp.taskErrors))
	copy(errs, wp.taskErrors)
	return errs
}

// Wait waits for all accepted tasks to finish processing.
func (wp *WorkerPool) Wait() {
	wp.taskWg.Wait()
}

// PoolState represents the public lifecycle state of a worker pool.
type PoolState int

const (
	// PoolStateNew indicates the pool has been created but not started.
	PoolStateNew PoolState = iota
	// PoolStateRunning indicates the pool is accepting and processing tasks.
	PoolStateRunning
	// PoolStatePaused indicates the pool has paused new submissions but running tasks continue.
	PoolStatePaused
	// PoolStateDraining indicates the pool is finishing queued work and rejecting new submissions.
	PoolStateDraining
	// PoolStateStopped indicates the pool has stopped.
	PoolStateStopped
)

// String returns a human-readable name for the pool state.
func (s PoolState) String() string {
	switch s {
	case PoolStateNew:
		return "new"
	case PoolStateRunning:
		return "running"
	case PoolStatePaused:
		return "paused"
	case PoolStateDraining:
		return "draining"
	case PoolStateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// PoolStatus is a snapshot of the worker pool's current state.
type PoolStatus struct {
	State       PoolState
	Workers     int
	QueuedTasks int
	TotalErrors int
}

func mapState(s workerPoolState) PoolState {
	switch s {
	case workerPoolStateNew:
		return PoolStateNew
	case workerPoolStateRunning:
		return PoolStateRunning
	case workerPoolStatePaused:
		return PoolStatePaused
	case workerPoolStateDraining:
		return PoolStateDraining
	case workerPoolStateStopped:
		return PoolStateStopped
	default:
		return PoolStateStopped
	}
}

// Status returns a snapshot of the pool's current state.
func (wp *WorkerPool) Status() PoolStatus {
	wp.mu.Lock()
	state := wp.state
	wp.mu.Unlock()

	wp.errorsMu.Lock()
	errCount := len(wp.taskErrors)
	wp.errorsMu.Unlock()

	return PoolStatus{
		State:       mapState(state),
		Workers:     wp.workers,
		QueuedTasks: len(wp.queue),
		TotalErrors: errCount,
	}
}

// Pause stops the pool from accepting new task submissions.
// Already-running and queued tasks continue to execute.
// Blocked submitters receive a "worker pool paused" error.
func (wp *WorkerPool) Pause() error {
	wp.mu.Lock()
	switch wp.state {
	case workerPoolStateRunning:
		wp.state = workerPoolStatePaused
		wp.closeAcceptingLocked()
		wp.mu.Unlock()
		wp.emit(Event{Type: EventPoolPaused})
		return nil
	case workerPoolStatePaused:
		wp.mu.Unlock()
		return errors.New("worker pool already paused")
	default:
		wp.mu.Unlock()
		return errors.New("worker pool cannot be paused in current state")
	}
}

// Resume re-opens the pool for new task submissions after a Pause.
func (wp *WorkerPool) Resume() error {
	wp.mu.Lock()
	if wp.state != workerPoolStatePaused {
		wp.mu.Unlock()
		return errors.New("worker pool is not paused")
	}

	wp.state = workerPoolStateRunning
	wp.acceptingDone = make(chan struct{})
	wp.acceptingClosed = false
	wp.closedSignal.Store(false)
	wp.mu.Unlock()
	wp.emit(Event{Type: EventPoolResumed})
	return nil
}

func (wp *WorkerPool) emit(event Event) {
	if wp.eventCh == nil {
		return
	}

	select {
	case wp.eventCh <- event:
	default:
	}
}
