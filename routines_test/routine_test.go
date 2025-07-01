package routines_test

import (
	"context"
	"github.com/pixie-sh/routines-go/routines"
	"testing"
	"time"
)

func TestGo(t *testing.T) {
	pool, _ := routines.NewWorkerPool(context.Background(), 1, 1)
	pool.Start()
	defer pool.Stop()

	done := make(chan struct{})
	pool.Go(func() {
		close(done)
	})

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Test timed out")
	}
}

func TestGoCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pool, _ := routines.NewWorkerPool(ctx, 1, 1)
	pool.Start()
	defer pool.Stop()

	done := make(chan struct{})
	pool.GoCtx(ctx, func() {
		close(done)
	})

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Test timed out")
	}

	cancel()
}
