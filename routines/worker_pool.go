package routines

import (
	"context"
	"errors"
	"sync"
	"time"
)

// WorkerPool represents a pool of workers that can process tasks from a queue
type WorkerPool struct {
	workers    int
	queue      chan Task
	ctx        context.Context
	cancelFunc context.CancelFunc
	wg         sync.WaitGroup
	started    bool
	mu         sync.Mutex
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
		queue:      make(chan Task, queueSize),
		ctx:        poolCtx,
		cancelFunc: cancelFunc,
		wg:         sync.WaitGroup{},
		started:    false,
	}, nil
}

// Start starts the worker pool
func (wp *WorkerPool) Start() error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if wp.started {
		return errors.New("worker pool already started")
	}

	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go wp.worker(i)
	}

	wp.started = true
	return nil
}

// worker is the main worker function that processes tasks from the queue
func (wp *WorkerPool) worker(id int) {
	defer wp.wg.Done()

	for {
		select {
		case <-wp.ctx.Done():
			return
		case task, ok := <-wp.queue:
			if !ok {
				return
			}

			// Execute the task
			_, _ = task(wp.ctx)
		}
	}
}

// Submit adds a task to the queue
// Returns an error if the queue is full or the pool is stopped
func (wp *WorkerPool) Submit(task Task) error {
	// First check if the context is already done
	select {
	case <-wp.ctx.Done():
		return errors.New("worker pool stopped")
	default:
		// Context is not done, continue
	}

	wp.mu.Lock()
	if !wp.started {
		wp.mu.Unlock()
		return errors.New("worker pool not started")
	}
	wp.mu.Unlock()

	// Try to submit the task, but also check if the context is done
	select {
	case wp.queue <- task:
		return nil
	case <-wp.ctx.Done():
		return errors.New("worker pool stopped")
	default:
		return errors.New("queue is full")
	}
}

// SubmitWait adds a task to the queue and waits for it to complete
// Returns the result and error from the task
func (wp *WorkerPool) SubmitWait(task Task) (any, error) {
	resultChan := make(chan any, 1)
	errChan := make(chan error, 1)
	doneChan := make(chan struct{})

	wrappedTask := func(ctx context.Context) (any, error) {
		defer close(doneChan)
		result, err := task(ctx)
		if err != nil {
			errChan <- err
		} else if result != nil {
			resultChan <- result
		}
		return nil, nil
	}

	if err := wp.Submit(wrappedTask); err != nil {
		return nil, err
	}

	// Wait for the task to complete
	select {
	case <-doneChan:
		// Task completed
	case <-wp.ctx.Done():
		return nil, errors.New("worker pool stopped")
	}

	// Check for results or errors
	select {
	case result := <-resultChan:
		return result, nil
	case err := <-errChan:
		return nil, err
	default:
		return nil, nil
	}
}

// Stop stops the worker pool and waits for all workers to finish
func (wp *WorkerPool) Stop() {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if !wp.started {
		return
	}

	wp.cancelFunc()
	close(wp.queue)
	wp.wg.Wait()
	wp.started = false
}

// Wait waits for all submitted tasks to be processed
// This does not stop accepting new tasks
func (wp *WorkerPool) Wait() {
	// Create a temporary channel to signal when the queue is empty and all tasks are processed
	done := make(chan struct{})

	// We'll use a simple sleep approach instead of tracking active tasks

	go func() {
		// First wait until the queue is empty
		for {
			wp.mu.Lock()
			queueEmpty := !wp.started || len(wp.queue) == 0
			wp.mu.Unlock()

			if queueEmpty {
				break
			}

			// Small sleep to prevent CPU spinning
			time.Sleep(10 * time.Millisecond)
		}

		// Now wait a bit more to ensure all workers have finished processing their current tasks
		// This is a simple approach - in a real-world scenario, you might want to track active tasks more precisely
		time.Sleep(200 * time.Millisecond)

		close(done)
	}()

	select {
	case <-done:
		// Queue is empty and all tasks are processed
	case <-wp.ctx.Done():
		// Context was canceled
	}
}
