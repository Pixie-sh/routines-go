package routines_test

import (
	"context"
	"errors"
	"github.com/pixie-sh/routines-go/routines"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPool_Basic(t *testing.T) {
	// Create a worker pool with 2 workers and a queue size of 5
	pool, err := routines.NewWorkerPool(context.Background(), 2, 5)
	if err != nil {
		t.Fatalf("Failed to create worker pool: %v", err)
	}

	// Start the worker pool
	if err := pool.Start(); err != nil {
		t.Fatalf("Failed to start worker pool: %v", err)
	}
	defer pool.Stop()

	// Counter to track completed tasks
	var counter int32

	// Submit 5 tasks
	for i := 0; i < 5; i++ {
		err := pool.Submit(func(ctx context.Context) (any, error) {
			time.Sleep(100 * time.Millisecond) // Simulate work
			atomic.AddInt32(&counter, 1)
			return nil, nil
		})
		if err != nil {
			t.Fatalf("Failed to submit task: %v", err)
		}
	}

	// Wait for all tasks to complete
	pool.Wait()

	// Check that all tasks were executed
	if atomic.LoadInt32(&counter) != 5 {
		t.Fatalf("Expected 5 tasks to be executed, got %d", counter)
	}
}

func TestWorkerPool_SubmitWait(t *testing.T) {
	// Create a worker pool with 1 worker and a queue size of 1
	pool, err := routines.NewWorkerPool(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("Failed to create worker pool: %v", err)
	}

	// Start the worker pool
	if err := pool.Start(); err != nil {
		t.Fatalf("Failed to start worker pool: %v", err)
	}
	defer pool.Stop()

	// Submit a task and wait for its result
	result, err := pool.SubmitWait(func(ctx context.Context) (any, error) {
		return "test result", nil
	})

	if err != nil {
		t.Fatalf("Task returned an error: %v", err)
	}

	if result != "test result" {
		t.Fatalf("Expected result 'test result', got '%v'", result)
	}
}

func TestWorkerPool_Error(t *testing.T) {
	// Create a worker pool with 1 worker and a queue size of 1
	pool, err := routines.NewWorkerPool(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("Failed to create worker pool: %v", err)
	}

	// Start the worker pool
	if err := pool.Start(); err != nil {
		t.Fatalf("Failed to start worker pool: %v", err)
	}
	defer pool.Stop()

	// Submit a task that returns an error
	_, err = pool.SubmitWait(func(ctx context.Context) (any, error) {
		return nil, errors.New("test error")
	})

	if err == nil {
		t.Fatal("Expected an error, got nil")
	}

	if err.Error() != "test error" {
		t.Fatalf("Expected error 'test error', got '%v'", err)
	}
}

func TestWorkerPool_QueueFull(t *testing.T) {
	// Create a worker pool with 1 worker and a queue size of 2
	pool, err := routines.NewWorkerPool(context.Background(), 1, 2)
	if err != nil {
		t.Fatalf("Failed to create worker pool: %v", err)
	}

	// Start the worker pool
	if err := pool.Start(); err != nil {
		t.Fatalf("Failed to start worker pool: %v", err)
	}
	defer pool.Stop()

	// Submit a task that blocks for a while
	err = pool.Submit(func(ctx context.Context) (any, error) {
		time.Sleep(500 * time.Millisecond) // Block the worker
		return nil, nil
	})
	if err != nil {
		t.Fatalf("Failed to submit first task: %v", err)
	}

	// Submit another task to fill the queue
	err = pool.Submit(func(ctx context.Context) (any, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("Failed to submit second task: %v", err)
	}

	// Try to submit a third task, which should fail because the queue is full
	err = pool.Submit(func(ctx context.Context) (any, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("Expected an error when submitting to a full queue, got nil")
	}

	if err.Error() != "queue is full" {
		t.Fatalf("Expected error 'queue is full', got '%v'", err)
	}
}

func TestWorkerPool_Cancellation(t *testing.T) {
	// Create a context that can be canceled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a worker pool with the cancellable context
	pool, err := routines.NewWorkerPool(ctx, 1, 1)
	if err != nil {
		t.Fatalf("Failed to create worker pool: %v", err)
	}

	// Start the worker pool
	if err := pool.Start(); err != nil {
		t.Fatalf("Failed to start worker pool: %v", err)
	}
	defer pool.Stop()

	// Cancel the context
	cancel()

	// Try to submit a task after cancellation
	err = pool.Submit(func(ctx context.Context) (any, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("Expected an error when submitting to a canceled pool, got nil")
	}

	if err.Error() != "worker pool stopped" {
		t.Fatalf("Expected error 'worker pool stopped', got '%v'", err)
	}
}
