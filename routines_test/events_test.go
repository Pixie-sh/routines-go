package routines_test

import (
	"context"
	"testing"
	"time"

	"github.com/pixie-sh/routines-go"
)

func collectEvents(ch <-chan routines.Event, timeout time.Duration) []routines.Event {
	var events []routines.Event
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, ev)
		case <-deadline:
			return events
		}
	}
}

func requireEvent(t *testing.T, ch <-chan routines.Event, want routines.EventType) routines.Event {
	t.Helper()
	select {
	case ev := <-ch:
		if ev.Type != want {
			t.Fatalf("event type = %v, want %v", ev.Type, want)
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for event %v", want)
		return routines.Event{}
	}
}

func noMoreEvents(t *testing.T, ch <-chan routines.Event) {
	t.Helper()
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event: %v", ev.Type)
	case <-time.After(50 * time.Millisecond):
	}
}

// --- Pool lifecycle events ---

func TestEvents_StartAndStop(t *testing.T) {
	events := make(chan routines.Event, 16)

	pool, err := routines.NewWorkerPool(context.Background(), 1, 1, routines.WithEvents(events))
	if err != nil {
		t.Fatalf("NewWorkerPool() error = %v", err)
	}

	if err := pool.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	requireEvent(t, events, routines.EventPoolStarted)

	pool.Stop()

	requireEvent(t, events, routines.EventPoolStopped)
}

func TestEvents_PauseAndResume(t *testing.T) {
	events := make(chan routines.Event, 16)

	pool, err := routines.NewWorkerPool(context.Background(), 1, 1, routines.WithEvents(events))
	if err != nil {
		t.Fatalf("NewWorkerPool() error = %v", err)
	}
	if err := pool.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(pool.Stop)

	requireEvent(t, events, routines.EventPoolStarted)

	if err := pool.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	requireEvent(t, events, routines.EventPoolPaused)

	if err := pool.Resume(); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	requireEvent(t, events, routines.EventPoolResumed)
}

func TestEvents_Drain(t *testing.T) {
	events := make(chan routines.Event, 16)

	pool, err := routines.NewWorkerPool(context.Background(), 1, 1, routines.WithEvents(events))
	if err != nil {
		t.Fatalf("NewWorkerPool() error = %v", err)
	}
	if err := pool.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	requireEvent(t, events, routines.EventPoolStarted)

	if err := pool.Drain(); err != nil {
		t.Fatalf("Drain() error = %v", err)
	}

	requireEvent(t, events, routines.EventPoolDraining)
	requireEvent(t, events, routines.EventPoolStopped)
}

// --- Task events ---

func TestEvents_TaskStartedAndCompleted(t *testing.T) {
	events := make(chan routines.Event, 16)

	pool, err := routines.NewWorkerPool(context.Background(), 1, 1, routines.WithEvents(events))
	if err != nil {
		t.Fatalf("NewWorkerPool() error = %v", err)
	}
	if err := pool.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(pool.Stop)

	requireEvent(t, events, routines.EventPoolStarted)

	resultChan, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	awaitTaskResult(t, resultChan)

	requireEvent(t, events, routines.EventTaskStarted)

	ev := requireEvent(t, events, routines.EventTaskCompleted)
	if ev.Error != nil {
		t.Fatalf("completed event should have nil error for successful task, got %v", ev.Error)
	}
}

func TestEvents_TaskCompletedWithError(t *testing.T) {
	events := make(chan routines.Event, 16)

	pool, err := routines.NewWorkerPool(context.Background(), 1, 1, routines.WithEvents(events))
	if err != nil {
		t.Fatalf("NewWorkerPool() error = %v", err)
	}
	if err := pool.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(pool.Stop)

	requireEvent(t, events, routines.EventPoolStarted)

	resultChan, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		return nil, context.DeadlineExceeded
	})
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	awaitTaskResult(t, resultChan)

	requireEvent(t, events, routines.EventTaskStarted)

	ev := requireEvent(t, events, routines.EventTaskCompleted)
	if ev.Error == nil {
		t.Fatal("completed event should carry the task error")
	}
}

func TestEvents_TaskPanicked(t *testing.T) {
	events := make(chan routines.Event, 16)

	pool, err := routines.NewWorkerPool(context.Background(), 1, 1, routines.WithEvents(events))
	if err != nil {
		t.Fatalf("NewWorkerPool() error = %v", err)
	}
	if err := pool.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(pool.Stop)

	requireEvent(t, events, routines.EventPoolStarted)

	resultChan, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		panic("event boom")
	})
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	awaitTaskResult(t, resultChan)

	requireEvent(t, events, routines.EventTaskStarted)

	panicEv := requireEvent(t, events, routines.EventTaskPanicked)
	if panicEv.Error == nil {
		t.Fatal("panic event should carry an error")
	}

	completedEv := requireEvent(t, events, routines.EventTaskCompleted)
	if completedEv.Error == nil {
		t.Fatal("completed event after panic should carry an error")
	}
}

// --- Drop semantics ---

func TestEvents_DroppedWhenChannelFull(t *testing.T) {
	events := make(chan routines.Event, 1)

	pool, err := routines.NewWorkerPool(context.Background(), 1, 4, routines.WithEvents(events))
	if err != nil {
		t.Fatalf("NewWorkerPool() error = %v", err)
	}
	if err := pool.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	for i := 0; i < 5; i++ {
		ch, submitErr := pool.AddTask(func(ctx context.Context) (interface{}, error) {
			return nil, nil
		})
		if submitErr != nil {
			t.Fatalf("AddTask() error = %v", submitErr)
		}
		awaitTaskResult(t, ch)
	}

	pool.Wait()
	pool.Stop()

	collected := collectEvents(events, 100*time.Millisecond)
	totalPossible := 1 + (5 * 2) + 1 // start + 5*(started+completed) + stop
	if len(collected) >= totalPossible {
		t.Fatalf("expected some events to be dropped with buffer=1, got %d/%d", len(collected), totalPossible)
	}
}

func TestEvents_NoEventsWithoutChannel(t *testing.T) {
	pool := newStartedWorkerPool(t, 1, 1)

	resultChan, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	awaitTaskResult(t, resultChan)
	pool.Wait()
	// No panic or error means emit is safely a no-op without a channel.
}

// --- EventType.String ---

func TestEventType_String(t *testing.T) {
	cases := []struct {
		et   routines.EventType
		want string
	}{
		{routines.EventPoolStarted, "pool.started"},
		{routines.EventPoolPaused, "pool.paused"},
		{routines.EventPoolResumed, "pool.resumed"},
		{routines.EventPoolDraining, "pool.draining"},
		{routines.EventPoolStopped, "pool.stopped"},
		{routines.EventTaskStarted, "task.started"},
		{routines.EventTaskCompleted, "task.completed"},
		{routines.EventTaskPanicked, "task.panicked"},
		{routines.EventType(99), "unknown"},
	}

	for _, tc := range cases {
		if got := tc.et.String(); got != tc.want {
			t.Errorf("EventType(%d).String() = %q, want %q", tc.et, got, tc.want)
		}
	}
}
