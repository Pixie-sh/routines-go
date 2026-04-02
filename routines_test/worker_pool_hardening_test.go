package routines_test

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pixie-sh/routines-go"
)

// =============================================================================
// T016 — Stress / Load
// =============================================================================

func TestHardening_ConcurrentSubmitters(t *testing.T) {
	t.Parallel()

	pool := newStartedWorkerPool(t, 10, 100)

	const submitters = 50
	const tasksPerSubmitter = 20
	const totalTasks = submitters * tasksPerSubmitter

	var completed int64
	var submitErrors int64

	var wg sync.WaitGroup
	wg.Add(submitters)

	for s := 0; s < submitters; s++ {
		go func() {
			defer wg.Done()
			for i := 0; i < tasksPerSubmitter; i++ {
				ch, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
					atomic.AddInt64(&completed, 1)
					return nil, nil
				})
				if err != nil {
					// Queue full is acceptable under contention; use blocking retry.
					ch, err = pool.AddTaskBlocking(context.Background(), func(ctx context.Context) (interface{}, error) {
						atomic.AddInt64(&completed, 1)
						return nil, nil
					})
					if err != nil {
						atomic.AddInt64(&submitErrors, 1)
						continue
					}
				}
				// Wait for result so we don't outrun the pool.
				awaitTaskResult(t, ch)
			}
		}()
	}

	wg.Wait()
	pool.Wait()

	if errors := atomic.LoadInt64(&submitErrors); errors != 0 {
		t.Fatalf("unexpected submission errors: %d", errors)
	}
	if got := atomic.LoadInt64(&completed); got != totalTasks {
		t.Fatalf("completed = %d, want %d", got, totalTasks)
	}
	if errs := pool.Errors(); len(errs) != 0 {
		t.Fatalf("expected no task errors, got %d", len(errs))
	}
}

func TestHardening_QueueSaturationAndRecovery(t *testing.T) {
	t.Parallel()

	// 1 worker, queue of 5. Block the single worker so the queue fills up.
	workerBlock := make(chan struct{})
	pool, err := routines.NewWorkerPool(context.Background(), 1, 5)
	if err != nil {
		t.Fatalf("NewWorkerPool() error = %v", err)
	}
	if err := pool.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(pool.Stop)

	// Block the worker.
	_, err = pool.AddTask(func(ctx context.Context) (interface{}, error) {
		<-workerBlock
		return nil, nil
	})
	if err != nil {
		t.Fatalf("AddTask(blocker) error = %v", err)
	}

	// Fill the queue (5 slots).
	for i := 0; i < 5; i++ {
		_, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
			return nil, nil
		})
		if err != nil {
			t.Fatalf("AddTask(fill %d) error = %v", i, err)
		}
	}

	// Next non-blocking add should fail with "queue is full".
	_, err = pool.AddTask(func(ctx context.Context) (interface{}, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("AddTask() should fail when queue is full")
	}
	if !strings.Contains(err.Error(), "queue is full") {
		t.Fatalf("expected 'queue is full' error, got: %v", err)
	}

	// Release the worker and verify recovery.
	close(workerBlock)
	pool.Wait()

	recoveryResult, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		return "recovered", nil
	})
	if err != nil {
		t.Fatalf("AddTask(recovery) error = %v", err)
	}
	result := awaitTaskResult(t, recoveryResult)
	if result.Result != "recovered" {
		t.Fatalf("recovery result = %v, want %q", result.Result, "recovered")
	}
}

func TestHardening_RapidLifecycleTransitions(t *testing.T) {
	t.Parallel()

	pool := newStartedWorkerPool(t, 4, 20)

	const cycles = 20
	for i := 0; i < cycles; i++ {
		if err := pool.Pause(); err != nil {
			t.Fatalf("Pause() cycle %d error = %v", i, err)
		}
		if err := pool.Resume(); err != nil {
			t.Fatalf("Resume() cycle %d error = %v", i, err)
		}

		// Submit a task after each cycle to verify the pool is functional.
		ch, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
			return i, nil
		})
		if err != nil {
			t.Fatalf("AddTask() cycle %d error = %v", i, err)
		}
		awaitTaskResult(t, ch)
	}

	pool.Wait()

	// Final verification: pool still accepts tasks.
	ch, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		return "final", nil
	})
	if err != nil {
		t.Fatalf("AddTask(final) error = %v", err)
	}
	result := awaitTaskResult(t, ch)
	if result.Result != "final" {
		t.Fatalf("final result = %v, want %q", result.Result, "final")
	}
}

// =============================================================================
// T017 — Soak
// =============================================================================

func TestHardening_ProlongedExecution(t *testing.T) {
	t.Parallel()

	pool := newStartedWorkerPool(t, 5, 50)

	const totalTasks = 500
	var completed int64
	results := make([]<-chan routines.TaskResult, 0, totalTasks)

	for i := 0; i < totalTasks; i++ {
		ch, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
			time.Sleep(1 * time.Millisecond)
			atomic.AddInt64(&completed, 1)
			return nil, nil
		})
		if err != nil {
			// Queue may be full; use blocking submit.
			ch, err = pool.AddTaskBlocking(context.Background(), func(ctx context.Context) (interface{}, error) {
				time.Sleep(1 * time.Millisecond)
				atomic.AddInt64(&completed, 1)
				return nil, nil
			})
			if err != nil {
				t.Fatalf("AddTask(%d) error = %v", i, err)
			}
		}
		results = append(results, ch)
	}

	// Wait for all results.
	for i, ch := range results {
		r := awaitTaskResult(t, ch)
		if r.Error != nil {
			t.Fatalf("task %d error = %v", i, r.Error)
		}
	}

	pool.Wait()

	if got := atomic.LoadInt64(&completed); got != totalTasks {
		t.Fatalf("completed = %d, want %d", got, totalTasks)
	}
	if errs := pool.Errors(); len(errs) != 0 {
		t.Fatalf("expected no errors, got %d", len(errs))
	}
}

func TestHardening_GoroutineLeakAfterStop(t *testing.T) {
	// Let the runtime settle before measuring baseline.
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	pool, err := routines.NewWorkerPool(context.Background(), 5, 10)
	if err != nil {
		t.Fatalf("NewWorkerPool() error = %v", err)
	}
	if err := pool.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Submit and complete some work.
	for i := 0; i < 20; i++ {
		ch, submitErr := pool.AddTask(func(ctx context.Context) (interface{}, error) {
			return nil, nil
		})
		if submitErr != nil {
			t.Fatalf("AddTask(%d) error = %v", i, submitErr)
		}
		awaitTaskResult(t, ch)
	}

	pool.Wait()
	pool.Stop()

	// Give goroutines a moment to wind down.
	time.Sleep(100 * time.Millisecond)
	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	after := runtime.NumGoroutine()
	tolerance := 5
	if after > baseline+tolerance {
		t.Fatalf("goroutine leak: before=%d, after=%d (tolerance +%d)", baseline, after, tolerance)
	}
}

func TestHardening_EventStreamConsistency(t *testing.T) {
	t.Parallel()

	const totalTasks = 100
	events := make(chan routines.Event, totalTasks*3)

	pool, err := routines.NewWorkerPool(context.Background(), 5, 50, routines.WithEvents(events))
	if err != nil {
		t.Fatalf("NewWorkerPool() error = %v", err)
	}
	if err := pool.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(pool.Stop)

	for i := 0; i < totalTasks; i++ {
		ch, submitErr := pool.AddTask(func(ctx context.Context) (interface{}, error) {
			return nil, nil
		})
		if submitErr != nil {
			ch, submitErr = pool.AddTaskBlocking(context.Background(), func(ctx context.Context) (interface{}, error) {
				return nil, nil
			})
			if submitErr != nil {
				t.Fatalf("AddTask(%d) error = %v", i, submitErr)
			}
		}
		awaitTaskResult(t, ch)
	}

	pool.Wait()
	pool.Stop()

	// Drain the event channel.
	collected := collectEvents(events, 500*time.Millisecond)

	var started, completed int
	for _, ev := range collected {
		switch ev.Type {
		case routines.EventTaskStarted:
			started++
		case routines.EventTaskCompleted:
			completed++
		}
	}

	if started != totalTasks {
		t.Fatalf("EventTaskStarted count = %d, want %d", started, totalTasks)
	}
	if completed != totalTasks {
		t.Fatalf("EventTaskCompleted count = %d, want %d", completed, totalTasks)
	}
}

// =============================================================================
// T018 — Failure Mode
// =============================================================================

func TestHardening_PanicBurst(t *testing.T) {
	t.Parallel()

	pool := newStartedWorkerPool(t, 5, 50)

	const panicCount = 50
	resultChans := make([]<-chan routines.TaskResult, panicCount)
	for i := 0; i < panicCount; i++ {
		msg := fmt.Sprintf("panic-%d", i)
		ch, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
			panic(msg)
		})
		if err != nil {
			ch, err = pool.AddTaskBlocking(context.Background(), func(ctx context.Context) (interface{}, error) {
				panic(msg)
			})
			if err != nil {
				t.Fatalf("AddTask(panic %d) error = %v", i, err)
			}
		}
		resultChans[i] = ch
	}

	// Wait for all panicking tasks to complete.
	for i, ch := range resultChans {
		r := awaitTaskResult(t, ch)
		if r.Error == nil {
			t.Fatalf("panic task %d should have returned an error", i)
		}
	}

	pool.Wait()

	errs := pool.Errors()
	if len(errs) != panicCount {
		t.Fatalf("Errors() count = %d, want %d", len(errs), panicCount)
	}

	// Pool should still be functional.
	ch, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		return "survived", nil
	})
	if err != nil {
		t.Fatalf("AddTask(post-panic) error = %v", err)
	}
	result := awaitTaskResult(t, ch)
	if result.Error != nil {
		t.Fatalf("post-panic task error = %v", result.Error)
	}
	if result.Result != "survived" {
		t.Fatalf("post-panic result = %v, want %q", result.Result, "survived")
	}
}

func TestHardening_CancellationStorm(t *testing.T) {
	t.Parallel()

	pool := newStartedWorkerPool(t, 5, 30)

	const taskCount = 30
	cancels := make([]context.CancelFunc, taskCount)
	resultChans := make([]<-chan routines.TaskResult, taskCount)

	for i := 0; i < taskCount; i++ {
		taskCtx, cancel := context.WithCancel(context.Background())
		cancels[i] = cancel
		ch, err := pool.AddTaskWithContext(taskCtx, func(ctx context.Context) (interface{}, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})
		if err != nil {
			t.Fatalf("AddTaskWithContext(%d) error = %v", i, err)
		}
		resultChans[i] = ch
	}

	// Cancel all at once.
	for _, cancel := range cancels {
		cancel()
	}

	// All tasks should complete (with errors).
	for i, ch := range resultChans {
		r := awaitTaskResult(t, ch)
		if r.Error == nil {
			t.Fatalf("cancelled task %d should have returned an error", i)
		}
	}

	pool.Wait()

	// Pool should remain functional.
	ch, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		return "still-alive", nil
	})
	if err != nil {
		t.Fatalf("AddTask(post-cancel) error = %v", err)
	}
	result := awaitTaskResult(t, ch)
	if result.Error != nil {
		t.Fatalf("post-cancel task error = %v", result.Error)
	}
	if result.Result != "still-alive" {
		t.Fatalf("post-cancel result = %v, want %q", result.Result, "still-alive")
	}
}

func TestHardening_DrainStopRace(t *testing.T) {
	t.Parallel()

	pool, err := routines.NewWorkerPool(context.Background(), 3, 10)
	if err != nil {
		t.Fatalf("NewWorkerPool() error = %v", err)
	}
	if err := pool.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Submit a few tasks to give Drain something to wait on.
	for i := 0; i < 5; i++ {
		_, submitErr := pool.AddTask(func(ctx context.Context) (interface{}, error) {
			time.Sleep(5 * time.Millisecond)
			return nil, nil
		})
		if submitErr != nil {
			t.Fatalf("AddTask(%d) error = %v", i, submitErr)
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = pool.Drain()
		}()
		go func() {
			defer wg.Done()
			pool.Stop()
		}()
		wg.Wait()
	}()

	select {
	case <-done:
		// Neither panicked nor deadlocked.
	case <-time.After(10 * time.Second):
		t.Fatal("DrainStopRace: deadlock detected (timeout)")
	}
}

func TestHardening_ObserverBackpressure(t *testing.T) {
	t.Parallel()

	// Buffer of 1 event channel — most events will be dropped.
	events := make(chan routines.Event, 1)

	pool, err := routines.NewWorkerPool(context.Background(), 3, 50, routines.WithEvents(events))
	if err != nil {
		t.Fatalf("NewWorkerPool() error = %v", err)
	}
	if err := pool.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(pool.Stop)

	const taskCount = 50
	resultChans := make([]<-chan routines.TaskResult, 0, taskCount)
	for i := 0; i < taskCount; i++ {
		ch, submitErr := pool.AddTask(func(ctx context.Context) (interface{}, error) {
			return nil, nil
		})
		if submitErr != nil {
			ch, submitErr = pool.AddTaskBlocking(context.Background(), func(ctx context.Context) (interface{}, error) {
				return nil, nil
			})
			if submitErr != nil {
				t.Fatalf("AddTask(%d) error = %v", i, submitErr)
			}
		}
		resultChans = append(resultChans, ch)
	}

	// Drain results — the pool must not deadlock despite the tiny event buffer.
	for i, ch := range resultChans {
		r := awaitTaskResult(t, ch)
		if r.Error != nil {
			t.Fatalf("task %d error = %v", i, r.Error)
		}
	}

	pool.Wait()
}
