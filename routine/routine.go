package routine

import (
	"context"
	"fmt"
	"sync"
)

var routine Routine = &goRoutine{
	wg: sync.WaitGroup{},
}

// Task represents a unit of work that can be executed by a goroutine.
// It returns an error if the execution fails.
type Task func(ctx context.Context) error

// Routine defines the interface for launching and waiting for goroutines.
type Routine interface {
	Launch(fn Task, ctx context.Context) <-chan error
	WaitUntil()
}

// goRoutine abstracts the use of go routines with wait group and error handling.
type goRoutine struct {
	wg sync.WaitGroup
}

// Go launches a routine with the provided function and no context.
func Go(fn Task) <-chan error {
	return routine.Launch(fn, context.Background())
}

// GoCtx launches a routine with the provided function and context.
func GoCtx(ctx context.Context, fn Task) <-chan error {
	return routine.Launch(fn, ctx)
}

// WaitUntil blocks until all launched routines are finished.
func WaitUntil() {
	routine.WaitUntil()
}

// Launch uses a goroutine to execute a Task.
// It handles panic recovery and context cancellation.
func (r *goRoutine) Launch(fn Task, ctx context.Context) <-chan error {
	r.wg.Add(1)
	errChan := make(chan error, 1) // Buffered to prevent goroutine leaks in case of unhandled errors.

	go func() {
		defer r.wg.Done()
		defer close(errChan)
		defer recoverPanic(errChan)

		select {
		case <-ctx.Done():
			errChan <- ctx.Err()
			return
		default:
			if err := fn(ctx); err != nil {
				errChan <- err
				return
			}
		}
	}()

	return errChan
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
