package routines

import (
	"context"
	"github.com/pixie-sh/logger-go/logger"
	"runtime"
	"time"
)

type memoryMonitor struct {
	Alloc,
	TotalAlloc,
	Sys,
	Mallocs,
	Frees,
	LiveObjects,
	PauseTotalNs uint64

	NumGC        uint32
	NumGoroutine int
}

// StartMemoryMonitoring start memory monitor
// blocking call
func StartMemoryMonitoring(ctx context.Context, durationSec int, plog logger.Interface) {
	var m memoryMonitor
	var rtm runtime.MemStats

	ticker := time.NewTicker(time.Duration(durationSec) * time.Second)
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-ticker.C:
			// Read full mem stats
			runtime.ReadMemStats(&rtm)

			// Number of goroutines
			m.NumGoroutine = runtime.NumGoroutine()

			// Misc memory stats
			m.Alloc = rtm.Alloc
			m.TotalAlloc = rtm.TotalAlloc
			m.Sys = rtm.Sys
			m.Mallocs = rtm.Mallocs
			m.Frees = rtm.Frees

			// Live objects = Mallocs - Frees
			m.LiveObjects = m.Mallocs - m.Frees

			// GC Stats
			m.PauseTotalNs = rtm.PauseTotalNs
			m.NumGC = rtm.NumGC

			plog.With("stats", m).Log("[mem stats]  %v", time.Now())
		}
	}
}
