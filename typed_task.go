package routines

import "context"

// TypedTask represents a task that returns a strongly typed result.
type TypedTask[T any] func(ctx context.Context) (T, error)

// TypedTaskResult represents the result of a typed task execution.
type TypedTaskResult[T any] struct {
	Result T
	Error  error
}

// AddTypedTask submits a typed task to the worker pool.
func AddTypedTask[T any](wp *WorkerPool, task TypedTask[T]) (<-chan TypedTaskResult[T], error) {
	return addTypedTask(wp.AddTask, task)
}

// AddTypedTaskWithContext submits a typed task with a task-specific context.
func AddTypedTaskWithContext[T any](wp *WorkerPool, taskCtx context.Context, task TypedTask[T]) (<-chan TypedTaskResult[T], error) {
	return addTypedTask(func(wrapped Task) (<-chan TaskResult, error) {
		return wp.AddTaskWithContext(taskCtx, wrapped)
	}, task)
}

// AddTypedTaskBlocking submits a typed task and waits for queue capacity.
func AddTypedTaskBlocking[T any](wp *WorkerPool, submitCtx context.Context, task TypedTask[T]) (<-chan TypedTaskResult[T], error) {
	return addTypedTask(func(wrapped Task) (<-chan TaskResult, error) {
		return wp.AddTaskBlocking(submitCtx, wrapped)
	}, task)
}

// AddTypedTaskAndWait submits a typed task and waits for its result.
func AddTypedTaskAndWait[T any](wp *WorkerPool, task TypedTask[T]) (T, error) {
	resultChan, err := AddTypedTask(wp, task)
	if err != nil {
		var zero T
		return zero, err
	}

	result := <-resultChan
	return result.Result, result.Error
}

func addTypedTask[T any](submit func(Task) (<-chan TaskResult, error), task TypedTask[T]) (<-chan TypedTaskResult[T], error) {
	untypedResultChan, err := submit(func(ctx context.Context) (interface{}, error) {
		result, taskErr := task(ctx)
		return result, taskErr
	})
	if err != nil {
		return nil, err
	}

	typedResultChan := make(chan TypedTaskResult[T], 1)
	go func() {
		defer close(typedResultChan)

		result, ok := <-untypedResultChan
		if !ok {
			return
		}

		if result.Error != nil {
			var zero T
			typedResultChan <- TypedTaskResult[T]{Result: zero, Error: result.Error}
			return
		}

		value, _ := result.Result.(T)
		typedResultChan <- TypedTaskResult[T]{Result: value, Error: nil}
	}()

	return typedResultChan, nil
}
