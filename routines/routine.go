package routines

import (
	"context"
	"github.com/pixie-sh/errors-go"
	"github.com/pixie-sh/logger-go/logger"
	"sync"
)

// Task represents a unit of work that can be executed by a goroutine.
// It returns an error if the execution fails.
type Task func(ctx context.Context) (any, error)
type SimpleTask func()

// Routines defines the interface for launching and waiting for goroutines.
type Routines interface {
	GoTask(Task) (<-chan any, <-chan error)
	GoTaskCtx(context.Context, Task) (<-chan any, <-chan error)

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

func (r *goRoutine) GoTaskCtx(ctx context.Context, task Task) (<-chan any, <-chan error) {
	return r.launch(ctx, task)
}

func (r *goRoutine) GoTask(task Task) (<-chan any, <-chan error) {
	return r.launch(r.ctx, task)
}

func (r *goRoutine) GoCtx(ctx context.Context, task SimpleTask) {
	_, _ = r.launch(ctx, func(ctx context.Context) (any, error) {
		task()
		return nil, nil
	})
}

func (r *goRoutine) Go(task SimpleTask) {
	_, _ = r.launch(r.ctx, func(ctx context.Context) (any, error) {
		task()
		return nil, nil
	})
}

// launch uses a goroutine to execute a Task.
// It handles panic recovery and context cancellation.
// channels are closed after task completion
func (r *goRoutine) launch(ctx context.Context, fn Task) (<-chan any, <-chan error) {
	errChan := make(chan error, 1)  // Buffered to prevent goroutine leaks in case of unhandled errors.
	returnChan := make(chan any, 1) // Buffered to prevent goroutine leaks in case of unhandled errors.

	go func() {
		r.wg.Add(1)

		defer close(errChan)
		defer close(returnChan)
		defer r.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				var err error
				switch x := r.(type) {
				case string:
					err = errors.New("panic: %v", x)
				case error:
					err = errors.New("panic: %v", x)
				default:
					err = errors.New("unknown panic: %v", x)
				}

				logger.Logger.With("error", r).Error(err.Error())
				_ = push(errChan, err)
			}
		}()

		select {
		case <-ctx.Done():
			push(errChan, ctx.Err())
		default:
			res, err := fn(ctx)
			if err != nil {
				_ = push(errChan, err)
			} else if res != nil {
				_ = push(returnChan, res)
			}
			// Always exit after executing the function once
		}
	}()

	return returnChan, errChan
}

// WaitUntil waits until all routines have finished. It's a blocking call.
func (r *goRoutine) WaitUntil() {
	r.wg.Wait()
}

func push[T any](ch chan<- T, msg T) bool {
	select {
	case ch <- msg:
		return true
	default:
		return false
	}
}
