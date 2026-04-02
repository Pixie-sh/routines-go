package routines

import (
	"context"
	"time"
)

type mergedTaskContext struct {
	poolCtx context.Context
	taskCtx context.Context
	done    chan struct{}
	errMu   chan struct{}
	err     error
}

func composeTaskContext(poolCtx context.Context, taskCtx context.Context) (context.Context, context.CancelFunc) {
	if taskCtx == nil {
		return context.WithCancel(poolCtx)
	}

	ctx := &mergedTaskContext{
		poolCtx: poolCtx,
		taskCtx: taskCtx,
		done:    make(chan struct{}),
		errMu:   make(chan struct{}, 1),
	}

	ctx.errMu <- struct{}{}

	stopPool := context.AfterFunc(poolCtx, func() {
		ctx.finish(poolCtx.Err())
	})
	stopTask := context.AfterFunc(taskCtx, func() {
		ctx.finish(taskCtx.Err())
	})

	return ctx, func() {
		stopPool()
		stopTask()
		ctx.finish(context.Canceled)
	}
}

func (ctx *mergedTaskContext) Deadline() (time.Time, bool) {
	poolDeadline, poolOK := ctx.poolCtx.Deadline()
	taskDeadline, taskOK := ctx.taskCtx.Deadline()

	switch {
	case !poolOK:
		return taskDeadline, taskOK
	case !taskOK:
		return poolDeadline, poolOK
	case taskDeadline.Before(poolDeadline):
		return taskDeadline, true
	default:
		return poolDeadline, true
	}
}

func (ctx *mergedTaskContext) Done() <-chan struct{} {
	return ctx.done
}

func (ctx *mergedTaskContext) Err() error {
	select {
	case <-ctx.done:
		return ctx.err
	default:
		return nil
	}
}

func (ctx *mergedTaskContext) Value(key interface{}) interface{} {
	if value := ctx.taskCtx.Value(key); value != nil {
		return value
	}

	return ctx.poolCtx.Value(key)
}

func (ctx *mergedTaskContext) finish(err error) {
	select {
	case <-ctx.done:
		return
	case <-ctx.errMu:
		ctx.err = err
		close(ctx.done)
	default:
	}
}
