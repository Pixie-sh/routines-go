package routines

import (
	"context"
	"fmt"
	"sync"
)

// Task represents a unit of work that can be executed by a goroutine.
// It returns an error if the execution fails.
type Task func(ctx context.Context) (any, error)
type SimpleTask func()

type ErrChan <-chan error
type ReturnChan <-chan any
type ExecTaskChan chan<- Task

// Routines defines the interface for launching and waiting for goroutines.
type Routines interface {
	GoTask(Task) (ExecTaskChan, ReturnChan, ErrChan)
	GoTaskCtx(context.Context, Task) (ExecTaskChan, ReturnChan, ErrChan)

	Go(task SimpleTask)
	GoCtx(context.Context, SimpleTask)

	WaitUntil()
}

// goRoutine abstracts the use of go routines with wait group and error handling.
type goRoutine struct {
	wg  sync.WaitGroup
	ctx context.Context
}

// NewRoutinesPool returns a default Routines implementation
func NewRoutinesPool(ctx context.Context) Routines {
	return &goRoutine{
		wg:  sync.WaitGroup{},
		ctx: ctx,
	}
}

func (r *goRoutine) GoTaskCtx(ctx context.Context, task Task) (ExecTaskChan, ReturnChan, ErrChan) {
	return r.launch(ctx, task)
}

func (r *goRoutine) GoTask(task Task) (ExecTaskChan, ReturnChan, ErrChan) {
	return r.launch(r.ctx, task)
}

func (r *goRoutine) GoCtx(ctx context.Context, task SimpleTask) {
	_, _, _ = r.launch(ctx, func(ctx context.Context) (any, error) {
		task()
		return nil, nil
	})
}

func (r *goRoutine) Go(task SimpleTask) {
	_, _, _ = r.launch(r.ctx, func(ctx context.Context) (any, error) {
		task()
		return nil, nil
	})
}

// Launch uses a goroutine to execute a Task.
// It handles panic recovery and context cancellation.
func (r *goRoutine) launch(ctx context.Context, fn Task) (ExecTaskChan, ReturnChan, ErrChan) {
	r.wg.Add(1)
	errChan := make(chan error, 1)  // Buffered to prevent goroutine leaks in case of unhandled errors.
	returnChan := make(chan any, 1) // Buffered to prevent goroutine leaks in case of unhandled errors.
	anotherTaskChan := make(chan Task, 1)

	go func() {
		defer r.wg.Done()
		defer close(errChan)
		defer recoverPanic(errChan)

		select {
		case <-ctx.Done():
			errChan <- ctx.Err()
			return
		case cmd := <-anotherTaskChan:
			res, err := cmd(ctx)
			if err != nil {
				errChan <- err

			} else if res != nil {
				returnChan <- res
			}

		default:
			res, err := fn(ctx)
			if err != nil {
				errChan <- err
				return
			} else if res != nil {
				returnChan <- res
			}
		}
	}()

	return anotherTaskChan, returnChan, errChan
}

// WaitUntil waits until all routines have finished. It's a blocking call.
func (r *goRoutine) WaitUntil() {
	r.wg.Wait()
}

// recoverPanic recovers from panics and sends an error on the provided channel.
func recoverPanic(errChan chan<- error) {
	if r := recover(); r != nil {
		var err error
		switch x := r.(type) {
		case string:
			err = fmt.Errorf("panic: %s", x)
		case error:
			err = fmt.Errorf("panic: %v", x)
		default:
			err = fmt.Errorf("unknown panic: %v", r)
		}
		errChan <- err
	}
}
