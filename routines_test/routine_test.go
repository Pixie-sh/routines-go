package routines_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestGo(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 1)

	done := make(chan struct{})
	err := callSimpleTaskMethodError(t, pool, "Go", context.Background(), func() {
		close(done)
	})
	if err != nil && !errors.Is(err, errSubmissionFailureHidden) {
		t.Fatalf("Go() error = %v", err)
	}

	waitWithTimeout(t, done, time.Second, "Go() task did not run")
}

func TestGo_ReturnsSubmissionFailureWhenQueueIsFull(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 1)

	releaseWorker := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseWorker) })
	})

	err := callSimpleTaskMethodError(t, pool, "Go", context.Background(), func() {
		<-releaseWorker
	})
	if err != nil && !errors.Is(err, errSubmissionFailureHidden) {
		t.Fatalf("Go() initial task error = %v", err)
	}

	queued := make(chan struct{})
	err = callSimpleTaskMethodError(t, pool, "Go", context.Background(), func() {
		close(queued)
	})
	if err != nil && !errors.Is(err, errSubmissionFailureHidden) {
		t.Fatalf("Go() queued task error = %v", err)
	}

	err = callSimpleTaskMethodError(t, pool, "Go", context.Background(), func() {})
	if err == nil {
		t.Fatal("Go() should surface queue submission failure when the pool is full")
	}
	if errors.Is(err, errSubmissionFailureHidden) {
		t.Fatal("Go() still hides submission failure because it does not return an error")
	}

	releaseOnce.Do(func() { close(releaseWorker) })
	waitWithTimeout(t, queued, time.Second, "queued Go() task did not run")
	pool.Wait()
}

func TestGoCtx(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 1)

	callerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	err := callSimpleTaskMethodError(t, pool, "GoCtx", callerCtx, func() {
		close(done)
	})
	if err != nil && !errors.Is(err, errSubmissionFailureHidden) {
		t.Fatalf("GoCtx() error = %v", err)
	}

	waitWithTimeout(t, done, time.Second, "GoCtx() task did not run")
}

func TestGoCtx_UsesCallerContextCancellation(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 2)

	callerCtx, cancel := context.WithCancel(context.Background())
	ctxObserved := make(chan error, 1)

	resultChan, err := callTaskSubmissionMethod(t, pool, "AddTaskWithContext", callerCtx, func(ctx context.Context) (interface{}, error) {
		<-ctx.Done()
		ctxObserved <- ctx.Err()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatalf("AddTaskWithContext() error = %v", err)
	}

	siblingDone := make(chan struct{})
	_, err = pool.AddTask(func(ctx context.Context) (interface{}, error) {
		close(siblingDone)
		return nil, nil
	})
	if err != nil {
		t.Fatalf("AddTask() sibling task error = %v", err)
	}

	cancel()

	select {
	case observed := <-ctxObserved:
		if !errors.Is(observed, context.Canceled) {
			t.Fatalf("observed task context error = %v, want context.Canceled", observed)
		}
	case <-time.After(time.Second):
		t.Fatal("task did not observe caller context cancellation")
	}

	result := awaitTaskResult(t, resultChan)
	if !errors.Is(result.Error, context.Canceled) {
		t.Fatalf("task result error = %v, want context.Canceled", result.Error)
	}

	waitWithTimeout(t, siblingDone, time.Second, "sibling task should continue after caller context cancellation")
	pool.Wait()
}

func TestGoCtx_ReturnsSubmissionFailureWhenQueueIsFull(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 1)

	releaseWorker := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseWorker) })
	})

	err := callSimpleTaskMethodError(t, pool, "GoCtx", context.Background(), func() {
		<-releaseWorker
	})
	if err != nil && !errors.Is(err, errSubmissionFailureHidden) {
		t.Fatalf("GoCtx() initial task error = %v", err)
	}

	err = callSimpleTaskMethodError(t, pool, "GoCtx", context.Background(), func() {})
	if err != nil && !errors.Is(err, errSubmissionFailureHidden) {
		t.Fatalf("GoCtx() queued task error = %v", err)
	}

	err = callSimpleTaskMethodError(t, pool, "GoCtx", context.Background(), func() {})
	if err == nil {
		t.Fatal("GoCtx() should surface queue submission failure when the pool is full")
	}
	if errors.Is(err, errSubmissionFailureHidden) {
		t.Fatal("GoCtx() still hides submission failure because it does not return an error")
	}

	releaseOnce.Do(func() { close(releaseWorker) })
	pool.Wait()
}
