package routines_tests

import (
	"context"
	"github.com/pixie-sh/logger-go/logger"
	routines2 "github.com/pixie-sh/routines-go/routines"
	"testing"
	"time"
)

func TestMemory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	routines2.Go(func() {
		routines2.StartMemoryMonitoring(ctx, 1, logger.Logger)
	})

	<-time.After(5 * time.Second)
	cancel()
}
