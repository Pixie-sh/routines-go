package routines

import "context"

// Task represents a unit of work that can be executed by a goroutine.
// It returns an error if the execution fails.
type Task func(ctx context.Context) (interface{}, error)
type SimpleTask func()

// Go submits a simple task to the worker pool.
func (wp *WorkerPool) Go(task SimpleTask) error {
	_, err := wp.AddTask(func(ctx context.Context) (interface{}, error) {
		task()
		return nil, nil
	})
	return err
}

// GoErr submits a simple task to the worker pool and returns submission errors.
func (wp *WorkerPool) GoErr(task SimpleTask) error {
	return wp.Go(task)
}

// GoCtx submits a simple task to the worker pool using a task-specific context.
func (wp *WorkerPool) GoCtx(ctx context.Context, task SimpleTask) error {
	_, err := wp.AddTaskWithContext(ctx, func(ctx context.Context) (interface{}, error) {
		task()
		return nil, nil
	})
	return err
}

// GoCtxErr submits a simple task to the worker pool using a task-specific context and returns submission errors.
func (wp *WorkerPool) GoCtxErr(ctx context.Context, task SimpleTask) error {
	return wp.GoCtx(ctx, task)
}
