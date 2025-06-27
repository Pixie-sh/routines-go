package routines

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/pixie-sh/errors-go"
)

// TaskResult represents the result of a task execution
type TaskResult struct {
	Result any
	Error  error
}

// WorkerPool represents a pool of workers that can process tasks from a queue
type WorkerPool struct {
	workers        int
	queue          chan Task
	ctx            context.Context
	cancelFunc     context.CancelFunc
	started        int32 // 0 = not started, 1 = started
	resultsChannel chan TaskResult
	routines       Routines
	pollInterval   time.Duration // Time between queue empty checks (default 10ms)
	gracePeriod    time.Duration // Grace period for in-progress tasks (default 100ms)
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
		workers:        workers,
		queue:          make(chan Task, queueSize),
		ctx:            poolCtx,
		cancelFunc:     cancelFunc,
		started:        0,
		resultsChannel: make(chan TaskResult, queueSize),
		routines:       NewRoutinesPool(poolCtx),
		pollInterval:   10 * time.Millisecond,
		gracePeriod:    100 * time.Millisecond,
	}, nil
}

func (wp *WorkerPool) SetWaitTimeouts(pollInterval, gracePeriod time.Duration) {
	wp.pollInterval = pollInterval
	wp.gracePeriod = gracePeriod
}

// Start starts the worker pool
func (wp *WorkerPool) Start() error {
	if !atomic.CompareAndSwapInt32(&wp.started, 0, 1) {
		return errors.New("worker pool already started")
	}

	for i := 0; i < wp.workers; i++ {
		wp.routines.Go(func() {
			wp.taskConsumer(i)
		})
	}

	return nil
}

// taskConsumer is the main taskConsumer function that processes tasks from the queue
func (wp *WorkerPool) taskConsumer(id int) {
	for {
		select {
		case <-wp.ctx.Done():
			return
		case task, ok := <-wp.queue:
			if !ok {
				return
			}

			result, err := task(wp.ctx)
			wp.resultsChannel <- TaskResult{Result: result, Error: err}
		}
	}
}

// AddTask adds a task to the queue
// Returns an error if the queue is full or the pool is stopped
func (wp *WorkerPool) AddTask(task Task) error {
	select {
	case <-wp.ctx.Done():
		return errors.New("worker pool stopped")
	default:
	}

	select {
	case wp.queue <- task:
		return nil
	case <-wp.ctx.Done():
		return errors.New("worker pool stopped")
	default:
		return errors.New("queue is full")
	}
}

// AddTaskAndWait adds a task to the queue and waits for it to complete
// Returns the result and error from the task
func (wp *WorkerPool) AddTaskAndWait(task Task) (any, error) {
	resultChan := make(chan any, 1)
	errChan := make(chan error, 1)

	// create a wrapped task that will be submitted to the queue
	wrappedTask := func(ctx context.Context) (any, error) {
		result, err := task(ctx)
		if err != nil {
			select {
			case errChan <- err:
			default:
			}
		} else if result != nil {
			select {
			case resultChan <- result:
			default:
			}
		}
		return nil, nil
	}

	// AddTask the wrapped task to the queue
	if err := wp.AddTask(wrappedTask); err != nil {
		return nil, err
	}

	// Use the Routines abstraction to wait for the result
	doneChan := make(chan struct{})
	wp.routines.Go(func() {
		defer close(doneChan)
		select {
		case <-wp.ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
			// Give the task some time to be processed
		}
	})

	select {
	case result := <-resultChan:
		return result, nil
	case err := <-errChan:
		return nil, err
	case <-doneChan:
		select {
		case result := <-resultChan:
			return result, nil
		case err := <-errChan:
			return nil, err
		default:
			return nil, nil
		}
	case <-wp.ctx.Done():
		return nil, errors.New("worker pool stopped")
	}
}

// Stop stops the worker pool and waits for all workers to finish
func (wp *WorkerPool) Stop() {
	if !atomic.CompareAndSwapInt32(&wp.started, 1, 0) {
		return
	}

	wp.cancelFunc()
	close(wp.queue)
	wp.routines.WaitUntil()
	close(wp.resultsChannel)
}

// GetResultsChan returns a channel that provides the results of tasks
// This channel will contain both results and errors from tasks
func (wp *WorkerPool) GetResultsChan() <-chan TaskResult {
	return wp.resultsChannel
}

// Wait waits for all submitted tasks to be processed
// This does not stop accepting new tasks
func (wp *WorkerPool) Wait() {
	// Create a temporary channel to signal when the queue is empty
	done := make(chan struct{})

	go func() {
		// First wait until the queue is empty
		for {
			queueEmpty := atomic.LoadInt32(&wp.started) == 0 || len(wp.queue) == 0

			if queueEmpty {
				break
			}

			time.Sleep(wp.pollInterval)
		}
		time.Sleep(wp.gracePeriod)

		close(done)
	}()

	// Wait for the queue to be empty and in-progress tasks to complete
	select {
	case <-done:
		// All tasks have been processed
	case <-wp.ctx.Done():
		// Context was canceled
	}
}
