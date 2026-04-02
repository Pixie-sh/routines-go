package routines_test

import (
	"context"
	"github.com/pixie-sh/routines-go"
	"testing"
	"time"
)

func BenchmarkWorkerPool(b *testing.B) {
	ctx := context.Background()
	pool, err := routines.NewWorkerPool(ctx, 10, b.N)
	if err != nil {
		b.Fatalf("Failed to create worker pool: %v", err)
	}

	if err := pool.Start(); err != nil {
		b.Fatalf("Failed to start worker pool: %v", err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		resultChan, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
			time.Sleep(1 * time.Millisecond)
			return nil, nil
		})
		if err != nil {
			b.Fatalf("AddTask() failed at iteration %d: %v", i, err)
		}

		select {
		case result := <-resultChan:
			if result.Error != nil {
				b.Fatalf("task error at iteration %d: %v", i, result.Error)
			}
		case <-time.After(5 * time.Second):
			b.Fatalf("timeout waiting for task result at iteration %d", i)
		}
	}

	b.StopTimer()
	pool.Wait()
	pool.Stop()
}
