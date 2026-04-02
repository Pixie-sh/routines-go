package routines_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pixie-sh/routines-go"
)

// --- Pause / Resume ---

func TestWorkerPool_PauseRejectsNewSubmissions(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 2)

	if err := pool.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}

	_, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("AddTask() should fail after Pause()")
	}
}

func TestWorkerPool_PauseAllowsRunningTasksToComplete(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 2)

	taskStarted := make(chan struct{})
	taskRelease := make(chan struct{})
	resultChan, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		close(taskStarted)
		<-taskRelease
		return "done", nil
	})
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}

	<-taskStarted

	if err := pool.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}

	close(taskRelease)

	result := awaitTaskResult(t, resultChan)
	if result.Error != nil {
		t.Fatalf("task error = %v", result.Error)
	}
	if result.Result != "done" {
		t.Fatalf("task result = %v, want %q", result.Result, "done")
	}
}

func TestWorkerPool_PauseAllowsQueuedTasksToComplete(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 2)

	releaseFirst := make(chan struct{})
	_, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		<-releaseFirst
		return nil, nil
	})
	if err != nil {
		t.Fatalf("AddTask() first error = %v", err)
	}

	queuedChan, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		return "queued-result", nil
	})
	if err != nil {
		t.Fatalf("AddTask() queued error = %v", err)
	}

	if err := pool.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}

	close(releaseFirst)

	result := awaitTaskResult(t, queuedChan)
	if result.Error != nil {
		t.Fatalf("queued task error = %v", result.Error)
	}
	if result.Result != "queued-result" {
		t.Fatalf("queued task result = %v, want %q", result.Result, "queued-result")
	}
}

func TestWorkerPool_ResumeReopensSubmissions(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 2)

	if err := pool.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}

	_, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("AddTask() should fail while paused")
	}

	if err := pool.Resume(); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}

	resultChan, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		return "resumed", nil
	})
	if err != nil {
		t.Fatalf("AddTask() after Resume() error = %v", err)
	}

	result := awaitTaskResult(t, resultChan)
	if result.Error != nil {
		t.Fatalf("task error = %v", result.Error)
	}
	if result.Result != "resumed" {
		t.Fatalf("task result = %v, want %q", result.Result, "resumed")
	}

	pool.Wait()
}

func TestWorkerPool_PauseResumePauseCycle(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 2)

	for i := 0; i < 3; i++ {
		if err := pool.Pause(); err != nil {
			t.Fatalf("Pause() cycle %d error = %v", i, err)
		}
		if err := pool.Resume(); err != nil {
			t.Fatalf("Resume() cycle %d error = %v", i, err)
		}
	}

	resultChan, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		return "after-cycles", nil
	})
	if err != nil {
		t.Fatalf("AddTask() after cycles error = %v", err)
	}

	result := awaitTaskResult(t, resultChan)
	if result.Result != "after-cycles" {
		t.Fatalf("task result = %v, want %q", result.Result, "after-cycles")
	}

	pool.Wait()
}

func TestWorkerPool_PauseFromNonRunningStateReturnsError(t *testing.T) {
	pool, err := routines.NewWorkerPool(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("NewWorkerPool() error = %v", err)
	}

	if err := pool.Pause(); err == nil {
		t.Fatal("Pause() on new pool should return error")
	}
}

func TestWorkerPool_DoublePauseReturnsError(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 1)

	if err := pool.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	if err := pool.Pause(); err == nil {
		t.Fatal("second Pause() should return error")
	}
}

func TestWorkerPool_ResumeFromNonPausedStateReturnsError(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 1)

	if err := pool.Resume(); err == nil {
		t.Fatal("Resume() on running pool should return error")
	}
}

func TestWorkerPool_PauseUnblocksBlockedSubmitters(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 1)

	releaseWorker := make(chan struct{})
	_, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		<-releaseWorker
		return nil, nil
	})
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}

	_, err = pool.AddTask(func(ctx context.Context) (interface{}, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("AddTask() fill queue error = %v", err)
	}

	blockedErr := make(chan error, 1)
	go func() {
		_, submitErr := pool.AddTaskBlocking(context.Background(), func(ctx context.Context) (interface{}, error) {
			return nil, nil
		})
		blockedErr <- submitErr
	}()

	time.Sleep(50 * time.Millisecond)

	if err := pool.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}

	select {
	case err := <-blockedErr:
		if err == nil {
			t.Fatal("blocked submitter should receive an error after Pause()")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked submitter was not unblocked by Pause()")
	}

	close(releaseWorker)
	pool.Wait()
}

func TestWorkerPool_DrainFromPausedState(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 2)

	var completed int32
	resultChan, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		atomic.AddInt32(&completed, 1)
		return nil, nil
	})
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	awaitTaskResult(t, resultChan)

	if err := pool.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}

	if err := pool.Drain(); err != nil {
		t.Fatalf("Drain() from paused error = %v", err)
	}

	if got := atomic.LoadInt32(&completed); got != 1 {
		t.Fatalf("completed = %d, want 1", got)
	}
}

func TestWorkerPool_StopFromPausedState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := routines.NewWorkerPool(ctx, 1, 1)
	if err != nil {
		t.Fatalf("NewWorkerPool() error = %v", err)
	}
	if err := pool.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := pool.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}

	pool.Stop()

	status := pool.Status()
	if status.State != routines.PoolStateStopped {
		t.Fatalf("state after Stop() = %v, want stopped", status.State)
	}
}

// --- Status ---

func TestWorkerPool_StatusReflectsNewState(t *testing.T) {
	pool, err := routines.NewWorkerPool(context.Background(), 3, 5)
	if err != nil {
		t.Fatalf("NewWorkerPool() error = %v", err)
	}
	t.Cleanup(pool.Stop)

	status := pool.Status()
	if status.State != routines.PoolStateNew {
		t.Fatalf("state = %v, want new", status.State)
	}
	if status.Workers != 3 {
		t.Fatalf("workers = %d, want 3", status.Workers)
	}
}

func TestWorkerPool_StatusReflectsRunningState(t *testing.T) {
	pool := newStartedWorkerPool(t, 2, 4)

	status := pool.Status()
	if status.State != routines.PoolStateRunning {
		t.Fatalf("state = %v, want running", status.State)
	}
	if status.Workers != 2 {
		t.Fatalf("workers = %d, want 2", status.Workers)
	}
}

func TestWorkerPool_StatusReflectsPausedState(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 1)

	if err := pool.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}

	status := pool.Status()
	if status.State != routines.PoolStatePaused {
		t.Fatalf("state = %v, want paused", status.State)
	}
}

func TestWorkerPool_StatusReflectsStoppedState(t *testing.T) {
	pool, err := routines.NewWorkerPool(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("NewWorkerPool() error = %v", err)
	}
	if err := pool.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	pool.Stop()

	status := pool.Status()
	if status.State != routines.PoolStateStopped {
		t.Fatalf("state = %v, want stopped", status.State)
	}
}

func TestWorkerPool_StatusReportsErrorCount(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 4)

	for i := 0; i < 3; i++ {
		ch, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
			return nil, context.DeadlineExceeded
		})
		if err != nil {
			t.Fatalf("AddTask() error = %v", err)
		}
		awaitTaskResult(t, ch)
	}

	pool.Wait()

	status := pool.Status()
	if status.TotalErrors != 3 {
		t.Fatalf("total errors = %d, want 3", status.TotalErrors)
	}
}

func TestWorkerPool_PoolStateString(t *testing.T) {
	cases := []struct {
		state routines.PoolState
		want  string
	}{
		{routines.PoolStateNew, "new"},
		{routines.PoolStateRunning, "running"},
		{routines.PoolStatePaused, "paused"},
		{routines.PoolStateDraining, "draining"},
		{routines.PoolStateStopped, "stopped"},
		{routines.PoolState(99), "unknown"},
	}

	for _, tc := range cases {
		if got := tc.state.String(); got != tc.want {
			t.Errorf("PoolState(%d).String() = %q, want %q", tc.state, got, tc.want)
		}
	}
}
