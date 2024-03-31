package routines_tests

import (
	"context"
	"errors"
	routines2 "github.com/pixie-sh/routines-go/routines"
	"sync"
	"testing"
	"time"
)

var routines = routines2.NewRoutinesPool(context.Background())

func TestGoTaskAndGoTaskCtxExecution(t *testing.T) {
	testCases := []struct {
		description    string
		task           func(ctx context.Context) (any, error)
		withContext    bool
		expectError    bool
		expectedResult any
	}{
		{
			description:    "Simple task execution without context",
			task:           func(ctx context.Context) (any, error) { return "success", nil },
			withContext:    false,
			expectError:    false,
			expectedResult: "success",
		},
		{
			description:    "Simple task execution with context",
			task:           func(ctx context.Context) (any, error) { return "with context", nil },
			withContext:    true,
			expectError:    false,
			expectedResult: "with context",
		},
		{
			description: "Task execution with error",
			task:        func(ctx context.Context) (any, error) { return nil, errors.New("task error") },
			withContext: false,
			expectError: true,
		},
		{
			description: "Task execution with panic",
			task:        func(ctx context.Context) (any, error) { panic("unexpected panic"); return nil, nil },
			withContext: false,
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			var _, retChan, errChan = routines.GoTask(tc.task)
			if tc.withContext {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_, retChan, errChan = routines.GoTaskCtx(ctx, tc.task)
			}

			select {
			case err := <-errChan:
				if !tc.expectError {
					t.Errorf("Did not expect error but received: %v", err)
				}
			case result := <-retChan:
				if tc.expectError {
					t.Errorf("Expected error but received result: %v", result)
				} else if result != tc.expectedResult {
					t.Errorf("Expected result %v, got %v", tc.expectedResult, result)
				}
			case <-time.After(3 * time.Second):
				t.Error("Test timed out")
			}
		})
	}
}

func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	_, _, errChan := routines.GoTaskCtx(ctx, func(ctx context.Context) (any, error) {
		time.Sleep(1 * time.Second)
		return nil, nil
	})
	cancel()

	select {
	case err := <-errChan:
		if err != context.Canceled {
			t.Errorf("Expected context.Canceled error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Test timed out, expected context cancellation")
	}
}

func TestWaitUntil(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		routines.Go(func() {
			time.Sleep(500 * time.Millisecond)
		})
		routines.WaitUntil()
	}()

	done := make(chan bool)
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success if it reaches here
	case <-time.After(2 * time.Second):
		t.Errorf("WaitUntil did not properly wait for all tasks to finish")
	}
}
