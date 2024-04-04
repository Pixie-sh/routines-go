package routines

import (
	"context"
	"sync"
)

var routine Routines = &goRoutine{
	wg:  sync.WaitGroup{},
	ctx: context.Background(),
}

// GoTask launches a routine with the provided function and no context.
func GoTask(fn Task) (<-chan any, <-chan error) {
	return routine.GoTask(fn)
}

// GoTaskCtx launches a routine with the provided function and context.
func GoTaskCtx(ctx context.Context, fn Task) (<-chan any, <-chan error) {
	return routine.GoTaskCtx(ctx, fn)
}

// Go launches a routine with the provided function and no context.
func Go(fn SimpleTask) {
	routine.Go(fn)
}

// GoCtx launches a routine with the provided function and context.
func GoCtx(ctx context.Context, fn SimpleTask) {
	routine.GoCtx(ctx, fn)
}

// WaitUntil blocks until all launched routines are finished.
func WaitUntil() {
	routine.WaitUntil()
}
