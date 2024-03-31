package routine

import (
	"context"
	"github.com/pixie-sh/logger-go/logger"
	"testing"
	"time"
)

func TestMemory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	_ = GoCtx(ctx, func(ctx context.Context) error {
		StartMemoryMonitoring(ctx, 1, logger.Logger)
		return nil
	})

	<-time.After(5 * time.Second)
	cancel()
}
