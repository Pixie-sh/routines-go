package routines

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/pixie-sh/errors-go"
)

// TaskResult represents the result of a task execution
type TaskResult struct {
	Result interface{}
	Error  error
}

// WorkerPool represents a pool of workers that can process tasks from a queue
type WorkerPool struct {
	workers    int
	queue      chan func(context.Context) TaskResult
	ctx        context.Context
	cancelFunc context.CancelFunc
	started    int32 // 0 = not started, 1 = started
	wg         sync.WaitGroup
	taskWg     sync.WaitGroup // New WaitGroup for tracking individual tasks
}

// NewWorkerPool creates a new worker pool with the specified number of workers and queue size
func NewWorkerPool(ctx context.Context, workers int, queueSize int) (*WorkerPool, error) {
	if workers <= 0 {
		return nil, errors.New("number of workers must be greater than 0")
	}
	if queueSize <= 0 {
		return nil, errors.New("queue size must be greater than 0")
	}

	poolCtx, cancelFunc := context.WithCancel(ctx)

	return &WorkerPool{
		workers:    workers,
		queue:      make(chan func(context.Context) TaskResult, queueSize),
		ctx:        poolCtx,
		cancelFunc: cancelFunc,
		started:    0,
	}, nil
}

// Start starts the worker pool
func (wp *WorkerPool) Start() error {
	if !atomic.CompareAndSwapInt32(&wp.started, 0, 1) {
		return errors.New("worker pool already started")
	}

	wp.wg.Add(wp.workers)
	for i := 0; i < wp.workers; i++ {
		go wp.taskConsumer()
	}

	return nil
}

// taskConsumer is the main taskConsumer function that processes tasks from the queue
func (wp *WorkerPool) taskConsumer() {
	defer wp.wg.Done()
	for {
		select {
		case <-wp.ctx.Done():
			return
		case taskFunc, ok := <-wp.queue:
			if !ok {
				return
			}
			taskFunc(wp.ctx)
			wp.taskWg.Done() // Signal that this task is done
		}
	}
}

// AddTask adds a task to the queue
// Returns a channel that will receive the task's result, or an error if the queue is full or the pool is stopped
func (wp *WorkerPool) AddTask(task Task) (<-chan TaskResult, error) {
	resultChan := make(chan TaskResult, 1)

	select {
	case <-wp.ctx.Done():
		return nil, errors.New("worker pool stopped")
	default:
	}

	wrappedTaskFunc := func(ctx context.Context) TaskResult {
		result, err := task(ctx)
		taskResult := TaskResult{Result: result, Error: err}
		select {
		case resultChan <- taskResult:
		default:
		}
		return taskResult
	}

	wp.taskWg.Add(1) // Increment for each task added
	select {
	case wp.queue <- wrappedTaskFunc:
		return resultChan, nil
	case <-wp.ctx.Done():
		wp.taskWg.Done() // Decrement if context is done before adding to queue
		return nil, errors.New("worker pool stopped")
	default:
		wp.taskWg.Done() // Decrement if queue is full
		return nil, errors.New("queue is full")
	}
}

// AddTaskAndWait adds a task to the queue and waits for it to complete
// Returns the result and error from the task
func (wp *WorkerPool) AddTaskAndWait(task Task) (interface{}, error) {
	resultChan, err := wp.AddTask(task)
	if err != nil {
		return nil, err
	}

	select {
	case result := <-resultChan:
		return result.Result, result.Error
	case <-wp.ctx.Done():
		return nil, errors.New("worker pool stopped before task completion")
	}
}

// Stop stops the worker pool and waits for all workers to finish
func (wp *WorkerPool) Stop() {
	if !atomic.CompareAndSwapInt32(&wp.started, 1, 0) {
		return
	}

	close(wp.queue)
	wp.cancelFunc()
	wp.wg.Wait()
}

// Wait waits for all submitted tasks to be processed
// This does not stop accepting new tasks
func (wp *WorkerPool) Wait() {
	wp.taskWg.Wait()
}
