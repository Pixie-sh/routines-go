package routines_test

import (
	"context"
	"errors"
	"github.com/pixie-sh/routines-go/routines"
	"testing"
	"time"
)

func TestGoTask(t *testing.T) {
	r := routines.NewRoutinesPool(context.Background())

	resultChan, errChan := r.GoTask(func(ctx context.Context) (any, error) {
		return "result", nil
	})

	select {
	case err := <-errChan:
		t.Fatalf("Did not expect an error but got one: %v", err)
	case result := <-resultChan:
		if result != "result" {
			t.Fatalf("Expected 'result', got %v", result)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Test timed out")
	}
}

func TestGoTaskWithError(t *testing.T) {
	r := routines.NewRoutinesPool(context.Background())

	_, errChan := r.GoTask(func(ctx context.Context) (any, error) {
		return nil, errors.New("task error")
	})

	select {
	case err := <-errChan:
		if err == nil || err.Error() != "task error" {
			t.Fatalf("Expected 'task error', got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Test timed out")
	}
}

func TestGoTaskWithPanic(t *testing.T) {
	r := routines.NewRoutinesPool(context.Background())

	_, _ = r.GoTask(func(ctx context.Context) (any, error) {
		panic("test panic")
	})
}

func TestWaitUntil(t *testing.T) {
	r := routines.NewRoutinesPool(context.Background())

	done := make(chan struct{})
	go func() {
		r.Go(func() {
			time.Sleep(100 * time.Millisecond)
		})
		r.WaitUntil()
		done <- struct{}{}
	}()

	select {
	case <-done:
		// Test passed
	case <-time.After(1 * time.Second):
		t.Fatal("WaitUntil did not wait for the goroutine to finish")
	}
}

// Additional tests can be added to cover GoTaskCtx, GoCtx, and various edge cases.
