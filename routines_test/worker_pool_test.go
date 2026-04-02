package routines_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pixie-sh/routines-go"
)

var errSubmissionFailureHidden = errors.New("submission failure is not visible to caller")

func newStartedWorkerPool(t *testing.T, workers int, queueSize int) *routines.WorkerPool {
	t.Helper()

	pool, err := routines.NewWorkerPool(context.Background(), workers, queueSize)
	if err != nil {
		t.Fatalf("NewWorkerPool() error = %v", err)
	}

	if err := pool.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	t.Cleanup(pool.Stop)

	return pool
}

func awaitTaskResult(t *testing.T, ch <-chan routines.TaskResult) routines.TaskResult {
	t.Helper()

	select {
	case result := <-ch:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for task result")
		return routines.TaskResult{}
	}
}

func waitWithTimeout(t *testing.T, done <-chan struct{}, timeout time.Duration, message string) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal(message)
	}
}

func requireMethod(t *testing.T, target any, methodName string) reflect.Value {
	t.Helper()

	method := reflect.ValueOf(target).MethodByName(methodName)
	if !method.IsValid() {
		t.Fatalf("%T does not expose %s", target, methodName)
	}

	return method
}

func valueToError(t *testing.T, value reflect.Value, label string) error {
	t.Helper()

	if !value.IsValid() {
		t.Fatalf("%s is not valid", label)
	}
	if value.IsNil() {
		return nil
	}

	err, ok := value.Interface().(error)
	if !ok {
		t.Fatalf("%s has type %s, want error", label, value.Type())
	}

	return err
}

func valueToTaskResultChan(t *testing.T, value reflect.Value, label string) <-chan routines.TaskResult {
	t.Helper()

	switch ch := value.Interface().(type) {
	case <-chan routines.TaskResult:
		return ch
	case chan routines.TaskResult:
		return ch
	default:
		t.Fatalf("%s has type %T, want TaskResult channel", label, value.Interface())
		return nil
	}
}

func callTaskSubmissionMethod(t *testing.T, pool *routines.WorkerPool, methodName string, ctx context.Context, task routines.Task) (<-chan routines.TaskResult, error) {
	t.Helper()

	method := requireMethod(t, pool, methodName)
	methodType := method.Type()

	var args []reflect.Value
	switch methodType.NumIn() {
	case 1:
		args = []reflect.Value{reflect.ValueOf(task)}
	case 2:
		ctxValue := reflect.ValueOf(ctx)
		taskValue := reflect.ValueOf(task)
		if ctxValue.Type().AssignableTo(methodType.In(0)) && taskValue.Type().AssignableTo(methodType.In(1)) {
			args = []reflect.Value{ctxValue, taskValue}
		} else if taskValue.Type().AssignableTo(methodType.In(0)) && ctxValue.Type().AssignableTo(methodType.In(1)) {
			args = []reflect.Value{taskValue, ctxValue}
		} else {
			t.Fatalf("%s signature %s does not accept context and task", methodName, methodType)
		}
	default:
		t.Fatalf("%s has unexpected arity %d", methodName, methodType.NumIn())
	}

	results := method.Call(args)
	if len(results) != 2 {
		t.Fatalf("%s returned %d values, want 2", methodName, len(results))
	}

	return valueToTaskResultChan(t, results[0], methodName+" result"), valueToError(t, results[1], methodName+" error")
}

func callSimpleTaskMethodError(t *testing.T, pool *routines.WorkerPool, methodName string, ctx context.Context, task routines.SimpleTask) error {
	t.Helper()

	method := requireMethod(t, pool, methodName)
	methodType := method.Type()
	taskValue := reflect.ValueOf(task)

	var args []reflect.Value
	switch methodType.NumIn() {
	case 1:
		args = []reflect.Value{taskValue}
	case 2:
		ctxValue := reflect.ValueOf(ctx)
		if ctxValue.Type().AssignableTo(methodType.In(0)) && taskValue.Type().AssignableTo(methodType.In(1)) {
			args = []reflect.Value{ctxValue, taskValue}
		} else if taskValue.Type().AssignableTo(methodType.In(0)) && ctxValue.Type().AssignableTo(methodType.In(1)) {
			args = []reflect.Value{taskValue, ctxValue}
		} else {
			t.Fatalf("%s signature %s does not accept simple task inputs", methodName, methodType)
		}
	default:
		t.Fatalf("%s has unexpected arity %d", methodName, methodType.NumIn())
	}

	results := method.Call(args)
	switch len(results) {
	case 0:
		return errSubmissionFailureHidden
	case 1:
		return valueToError(t, results[0], methodName+" error")
	default:
		t.Fatalf("%s returned %d values, want 0 or 1", methodName, len(results))
		return nil
	}
}

func callNoArgMethodError(t *testing.T, pool *routines.WorkerPool, methodName string) error {
	t.Helper()

	method := requireMethod(t, pool, methodName)
	results := method.Call(nil)
	switch len(results) {
	case 0:
		return nil
	case 1:
		return valueToError(t, results[0], methodName+" error")
	default:
		t.Fatalf("%s returned %d values, want 0 or 1", methodName, len(results))
		return nil
	}
}

func callErrorsMethod(t *testing.T, pool *routines.WorkerPool) []error {
	t.Helper()

	method := requireMethod(t, pool, "Errors")
	results := method.Call(nil)
	if len(results) != 1 {
		t.Fatalf("Errors returned %d values, want 1", len(results))
	}

	errs, ok := results[0].Interface().([]error)
	if !ok {
		t.Fatalf("Errors returned %T, want []error", results[0].Interface())
	}

	return errs
}

func containsErrorSubstring(errs []error, want string) bool {
	for _, err := range errs {
		if err != nil && strings.Contains(err.Error(), want) {
			return true
		}
	}

	return false
}

func TestWorkerPool_Basic(t *testing.T) {
	pool := newStartedWorkerPool(t, 2, 5)

	var counter int32
	for i := 0; i < 5; i++ {
		resultChan, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
			atomic.AddInt32(&counter, 1)
			return nil, nil
		})
		if err != nil {
			t.Fatalf("AddTask() error = %v", err)
		}
		result := awaitTaskResult(t, resultChan)
		if result.Error != nil {
			t.Fatalf("task returned unexpected error: %v", result.Error)
		}
	}

	pool.Wait()

	if got := atomic.LoadInt32(&counter); got != 5 {
		t.Fatalf("executed tasks = %d, want 5", got)
	}
}

func TestWorkerPool_PanicRecoveryAndWorkerSurvival(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 2)

	firstResult, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		panic("boom")
	})
	if err != nil {
		t.Fatalf("AddTask() panicking task error = %v", err)
	}

	secondResult, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		return "survived", nil
	})
	if err != nil {
		t.Fatalf("AddTask() post-panic task error = %v", err)
	}

	first := awaitTaskResult(t, firstResult)
	if first.Error == nil {
		t.Fatal("expected panicking task to return an error")
	}

	second := awaitTaskResult(t, secondResult)
	if second.Error != nil {
		t.Fatalf("post-panic task error = %v", second.Error)
	}
	if second.Result != "survived" {
		t.Fatalf("post-panic task result = %v, want %q", second.Result, "survived")
	}

	pool.Wait()

	errList := callErrorsMethod(t, pool)
	if !containsErrorSubstring(errList, "boom") && !containsErrorSubstring(errList, "panic") {
		t.Fatalf("Errors() should include recovered panic, got: %v", errList)
	}
}

func TestWorkerPool_AddTaskAndWaitReturnsPanicError(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 1)

	result, err := pool.AddTaskAndWait(func(ctx context.Context) (interface{}, error) {
		panic("wait boom")
	})
	if err == nil {
		t.Fatal("expected AddTaskAndWait to return an error for panic")
	}
	if result != nil {
		t.Fatalf("expected nil result for panicking task, got %v", result)
	}
	if !strings.Contains(err.Error(), "wait boom") && !strings.Contains(err.Error(), "panic") {
		t.Fatalf("expected panic error text, got: %v", err)
	}
}

func TestWorkerPool_WithPanicHandlerReceivesRecoveredValue(t *testing.T) {
	panicSeen := make(chan interface{}, 1)
	stackSeen := make(chan []byte, 1)

	pool, err := routines.NewWorkerPool(context.Background(), 1, 1, routines.WithPanicHandler(func(recovered interface{}, stack []byte) {
		panicSeen <- recovered
		stackSeen <- append([]byte(nil), stack...)
	}))
	if err != nil {
		t.Fatalf("NewWorkerPool() error = %v", err)
	}
	if err := pool.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(pool.Stop)

	resultChan, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		panic("hook boom")
	})
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}

	result := awaitTaskResult(t, resultChan)
	if result.Error == nil {
		t.Fatal("expected panic result error")
	}

	select {
	case recovered := <-panicSeen:
		if recovered != "hook boom" {
			t.Fatalf("panic handler recovered = %v, want %q", recovered, "hook boom")
		}
	case <-time.After(time.Second):
		t.Fatal("panic handler was not invoked")
	}

	select {
	case stack := <-stackSeen:
		if len(stack) == 0 {
			t.Fatal("panic handler received empty stack")
		}
	case <-time.After(time.Second):
		t.Fatal("panic handler stack was not captured")
	}

	pool.Wait()
}

func TestWorkerPool_AddTaskBlockingWaitsForCapacity(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 1)

	releaseWorker := make(chan struct{})
	_, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		<-releaseWorker
		return nil, nil
	})
	if err != nil {
		t.Fatalf("AddTask() first task error = %v", err)
	}

	_, err = pool.AddTask(func(ctx context.Context) (interface{}, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("AddTask() second task error = %v", err)
	}

	blockedDone := make(chan routines.TaskResult, 1)
	blockedErr := make(chan error, 1)
	go func() {
		resultChan, callErr := callTaskSubmissionMethod(t, pool, "AddTaskBlocking", context.Background(), func(ctx context.Context) (interface{}, error) {
			return "third", nil
		})
		if callErr != nil {
			blockedErr <- callErr
			return
		}

		blockedDone <- awaitTaskResult(t, resultChan)
	}()

	select {
	case err := <-blockedErr:
		t.Fatalf("AddTaskBlocking returned early with error: %v", err)
	case <-blockedDone:
		t.Fatal("AddTaskBlocking returned before queue capacity was available")
	case <-time.After(150 * time.Millisecond):
	}

	close(releaseWorker)

	select {
	case err := <-blockedErr:
		t.Fatalf("AddTaskBlocking returned error after capacity opened: %v", err)
	case result := <-blockedDone:
		if result.Error != nil {
			t.Fatalf("AddTaskBlocking task error = %v", result.Error)
		}
		if result.Result != "third" {
			t.Fatalf("AddTaskBlocking result = %v, want %q", result.Result, "third")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AddTaskBlocking did not finish after capacity opened")
	}

	pool.Wait()
}

func TestWorkerPool_AddTaskBlockingHonorsContextCancellation(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 1)

	releaseWorker := make(chan struct{})
	_, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		<-releaseWorker
		return nil, nil
	})
	if err != nil {
		t.Fatalf("AddTask() first task error = %v", err)
	}

	_, err = pool.AddTask(func(ctx context.Context) (interface{}, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("AddTask() second task error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_, err = callTaskSubmissionMethod(t, pool, "AddTaskBlocking", ctx, func(ctx context.Context) (interface{}, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("AddTaskBlocking should return an error when the caller context is canceled")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("AddTaskBlocking error = %v, want context cancellation", err)
	}

	close(releaseWorker)
	pool.Wait()
}

func TestWorkerPool_AddTaskWithContextUsesTaskContext(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 1)

	taskCtx, cancel := context.WithCancel(context.Background())
	resultChan, err := callTaskSubmissionMethod(t, pool, "AddTaskWithContext", taskCtx, func(ctx context.Context) (interface{}, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatalf("AddTaskWithContext() error = %v", err)
	}

	cancel()

	result := awaitTaskResult(t, resultChan)
	if !errors.Is(result.Error, context.Canceled) {
		t.Fatalf("task error = %v, want context.Canceled", result.Error)
	}

	pool.Wait()
}

func TestWorkerPool_DrainRejectsNewTasksAndFinishesQueuedWork(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 2)

	releaseWorker := make(chan struct{})
	var completed int32

	_, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		<-releaseWorker
		atomic.AddInt32(&completed, 1)
		return nil, nil
	})
	if err != nil {
		t.Fatalf("AddTask() first task error = %v", err)
	}

	queuedResult, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		atomic.AddInt32(&completed, 1)
		return "queued", nil
	})
	if err != nil {
		t.Fatalf("AddTask() second task error = %v", err)
	}

	drainDone := make(chan error, 1)
	go func() {
		drainDone <- callNoArgMethodError(t, pool, "Drain")
	}()

	deadline := time.After(2 * time.Second)
	for {
		_, submitErr := pool.AddTask(func(ctx context.Context) (interface{}, error) {
			return nil, nil
		})
		if submitErr != nil {
			break
		}

		select {
		case <-deadline:
			t.Fatal("Drain() did not stop accepting new tasks")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	close(releaseWorker)

	queued := awaitTaskResult(t, queuedResult)
	if queued.Error != nil {
		t.Fatalf("queued task error = %v", queued.Error)
	}
	if queued.Result != "queued" {
		t.Fatalf("queued task result = %v, want %q", queued.Result, "queued")
	}

	select {
	case err := <-drainDone:
		if err != nil {
			t.Fatalf("Drain() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Drain() did not return after queued tasks finished")
	}

	if got := atomic.LoadInt32(&completed); got != 2 {
		t.Fatalf("completed tasks = %d, want 2", got)
	}
}

func TestWorkerPool_ErrorsAggregatesFailures(t *testing.T) {
	pool := newStartedWorkerPool(t, 2, 4)

	firstErr := errors.New("first failure")
	secondErr := errors.New("second failure")

	firstResult, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		return nil, firstErr
	})
	if err != nil {
		t.Fatalf("AddTask() first error task = %v", err)
	}

	secondResult, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		return nil, secondErr
	})
	if err != nil {
		t.Fatalf("AddTask() second error task = %v", err)
	}

	_ = awaitTaskResult(t, firstResult)
	_ = awaitTaskResult(t, secondResult)
	pool.Wait()

	errList := callErrorsMethod(t, pool)
	if len(errList) < 2 {
		t.Fatalf("Errors() returned %d errors, want at least 2", len(errList))
	}
	if !containsErrorSubstring(errList, firstErr.Error()) {
		t.Fatalf("Errors() missing %q: %v", firstErr, errList)
	}
	if !containsErrorSubstring(errList, secondErr.Error()) {
		t.Fatalf("Errors() missing %q: %v", secondErr, errList)
	}
}

func TestWorkerPool_TypedTaskHelpers(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 2)

	typedResult, err := routines.AddTypedTaskAndWait(pool, func(ctx context.Context) (int, error) {
		return 42, nil
	})
	if err != nil {
		t.Fatalf("AddTypedTaskAndWait() error = %v", err)
	}
	if typedResult != 42 {
		t.Fatalf("AddTypedTaskAndWait() result = %d, want 42", typedResult)
	}

	taskCtx, cancel := context.WithCancel(context.Background())
	resultChan, err := routines.AddTypedTaskWithContext(pool, taskCtx, func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	if err != nil {
		t.Fatalf("AddTypedTaskWithContext() error = %v", err)
	}
	cancel()

	select {
	case result := <-resultChan:
		if !errors.Is(result.Error, context.Canceled) {
			t.Fatalf("typed task error = %v, want context.Canceled", result.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for typed task result")
	}

	pool.Wait()
}
