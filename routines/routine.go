package routines

import (
	"context"
)

// Task represents a unit of work that can be executed by a goroutine.
// It returns an error if the execution fails.
type Task func(ctx context.Context) (interface{}, error)
type SimpleTask func()

func (wp *WorkerPool) Go(task SimpleTask) {
	_, _ = wp.AddTask(func(ctx context.Context) (interface{}, error) {
		task()
		return nil, nil
	})
}

func (wp *WorkerPool) GoCtx(ctx context.Context, task SimpleTask) {
	_, _ = wp.AddTask(func(ctx context.Context) (interface{}, error) {
		task()
		return nil, nil
	})
}
