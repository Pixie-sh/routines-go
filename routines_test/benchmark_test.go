package routines_test

import (
	"context"
	"github.com/pixie-sh/routines-go/routines"
	"testing"
	"time"
)

func BenchmarkWorkerPool(b *testing.B) {
	ctx := context.Background()
	pool, _ := routines.NewWorkerPool(ctx, 10, b.N)
	pool.Start()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = pool.AddTask(func(ctx context.Context) (interface{}, error) {
			time.Sleep(1 * time.Millisecond)
			return nil, nil
		})
	}

	pool.Stop()
}
