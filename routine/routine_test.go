package routine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestRoutineSuccess tests the successful execution of a goroutine task.
func TestRoutineSuccess(t *testing.T) {
	task := func(ctx context.Context) error {
		// Simulate work
		time.Sleep(100 * time.Millisecond)
		return nil // no error
	}

	errChan := Go(task)

	if err := <-errChan; err != nil {
		t.Errorf("Expected no error, but got: %v", err)
	}
}

// TestRoutineError tests proper error propagation from a goroutine task.
func TestRoutineError(t *testing.T) {
	expectedError := errors.New("expected error")
	task := func(ctx context.Context) error {
		return expectedError
	}

	errChan := Go(task)

	if err := <-errChan; err != expectedError {
		t.Errorf("Expected error '%v', but got: '%v'", expectedError, err)
	}
}

// TestRoutinePanicRecovery tests that the package properly recovers from panics in goroutine tasks.
func TestRoutinePanicRecovery(t *testing.T) {
	task := func(ctx context.Context) error {
		<-time.After(1 * time.Second)
		panic("test panic")
	}

	errChan := Go(task)

	err := <-errChan
	if err == nil {
		t.Errorf("Expected an error due to panic, but got nil")
	} else if !strings.Contains(err.Error(), "panic: test panic") {
		t.Errorf("Expected panic message to be 'panic: test panic', but got: '%v'", err)
	}
}

// TestRoutineCancellation tests that tasks respect context cancellation.
func TestRoutineCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	task := func(ctx context.Context) error {
		select {
		case <-time.After(1 * time.Second):
			return errors.New("task did not cancel")
		case <-ctx.Done():
			return nil // Expected result
		}
	}

	errChan := GoCtx(ctx, task)

	<-time.After(100 * time.Millisecond)
	// Cancel the context immediately
	cancel()

	if err := <-errChan; err != nil {
		t.Errorf("Expected no error after cancellation, but got: %v", err)
	}
}

// TestRoutineWaitUntil tests the WaitUntil functionality to ensure it waits for all tasks to complete.
func TestRoutineWaitUntil(t *testing.T) {
	start := time.Now()

	// Launch several tasks that take some time to complete
	for i := 0; i < 5; i++ {
		Go(func(ctx context.Context) error {
			time.Sleep(1 * time.Second)
			return nil
		})
	}

	WaitUntil()

	duration := time.Since(start)
	if duration < 1*time.Second {
		t.Errorf("Expected WaitUntil to wait for all tasks, but it returned in %v", duration)
	}

	if duration > 10*time.Second {
		t.Errorf("Expected WaitUntil to wait for all tasks, but it returned in %v", duration)
	}
}
