# routines-go

A lightweight Go worker pool library for bounded concurrency, task queuing, and goroutine lifecycle management. Built for production use with panic recovery, graceful shutdown, pause/resume control, and real-time event observability.

[![Go Reference](https://pkg.go.dev/badge/github.com/pixie-sh/routines-go.svg)](https://pkg.go.dev/github.com/pixie-sh/routines-go)

## Why routines-go?

- **Bounded concurrency** -- fixed worker count prevents goroutine leaks and unbounded resource usage
- **Production-safe** -- automatic panic recovery keeps workers alive after task failures
- **Full lifecycle control** -- start, pause, resume, drain, and stop with clean state transitions
- **Real-time observability** -- event channels for monitoring task and pool lifecycle transitions
- **Type-safe generics** -- typed task helpers eliminate manual type assertions
- **Context-aware** -- pool-scoped and per-task context propagation with cancellation support

## Installation

```bash
go get github.com/pixie-sh/routines-go
```

Requires Go 1.23+.

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/pixie-sh/routines-go"
)

func main() {
    pool, err := routines.NewWorkerPool(context.Background(), 4, 100)
    if err != nil {
        log.Fatal(err)
    }

    if err := pool.Start(); err != nil {
        log.Fatal(err)
    }
    defer pool.Stop()

    resultCh, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
        return "hello from worker pool", nil
    })
    if err != nil {
        log.Fatal(err)
    }

    result := <-resultCh
    fmt.Println(result.Result)
}
```

## API Reference

### Creating and Managing a Worker Pool

| Method | Description |
|--------|-------------|
| `NewWorkerPool(ctx, workers, queueSize, opts...)` | Create a new worker pool |
| `Start()` | Launch worker goroutines |
| `Stop()` | Cancel context, close queue, wait for workers to exit |
| `Wait()` | Block until all accepted tasks finish |
| `Drain()` | Reject new work, finish queued work, then stop |
| `Pause()` | Stop accepting submissions; running and queued tasks continue |
| `Resume()` | Re-open submissions after a pause |
| `Status()` | Return a `PoolStatus` snapshot (state, workers, queued tasks, error count) |
| `Errors()` | Return a snapshot of recorded task errors |

### Submitting Tasks

| Method | Description |
|--------|-------------|
| `AddTask(task)` | Submit a task, returns `<-chan TaskResult` |
| `AddTaskAndWait(task)` | Submit and block until result is ready |
| `AddTaskWithContext(ctx, task)` | Submit with a per-task context for cancellation |
| `AddTaskBlocking(ctx, task)` | Wait for queue capacity before submitting |
| `Go(task)` | Submit a simple `func()` task |
| `GoCtx(ctx, task)` | Submit a simple task with context |

### Options

| Option | Description |
|--------|-------------|
| `WithPanicHandler(fn)` | Register a callback for recovered task panics |
| `WithEvents(ch)` | Register a channel for pool and task lifecycle events |

### Typed Task Helpers (Generics)

| Function | Description |
|----------|-------------|
| `AddTypedTask[T](pool, task)` | Submit a typed task |
| `AddTypedTaskAndWait[T](pool, task)` | Submit and wait for a typed result |
| `AddTypedTaskWithContext[T](pool, ctx, task)` | Submit a typed task with context |
| `AddTypedTaskBlocking[T](pool, ctx, task)` | Blocking submit for a typed task |

### Core Types

```go
type Task func(ctx context.Context) (interface{}, error)

type TaskResult struct {
    Result interface{}
    Error  error
}

type PoolStatus struct {
    State       PoolState
    Workers     int
    QueuedTasks int
    TotalErrors int
}
```

## Usage Examples

### Submit and Wait Inline

```go
result, err := pool.AddTaskAndWait(func(ctx context.Context) (interface{}, error) {
    return 42, nil
})
if err != nil {
    log.Fatal(err)
}
fmt.Println(result) // 42
```

### Blocking Submit with Graceful Drain

```go
submitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
defer cancel()

resultCh, err := pool.AddTaskBlocking(submitCtx, func(ctx context.Context) (interface{}, error) {
    return "queued after capacity opened", nil
})
if err != nil {
    log.Fatal(err)
}

result := <-resultCh
fmt.Println(result.Result)

pool.Drain() // finish queued work, then stop
```

### Per-Task Context and Error Aggregation

```go
taskCtx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
defer cancel()

resultCh, _ := pool.AddTaskWithContext(taskCtx, func(ctx context.Context) (interface{}, error) {
    <-ctx.Done()
    return nil, ctx.Err()
})

result := <-resultCh
fmt.Println(result.Error) // context deadline exceeded

pool.Wait()
for _, err := range pool.Errors() {
    fmt.Println(err)
}
```

### Typed Tasks with Generics

```go
value, err := routines.AddTypedTaskAndWait(pool, func(ctx context.Context) (int, error) {
    return 42, nil
})
fmt.Println(value) // 42 (int, no type assertion needed)
```

### Panic Recovery

```go
pool, _ := routines.NewWorkerPool(ctx, 4, 100,
    routines.WithPanicHandler(func(recovered interface{}, stack []byte) {
        log.Printf("task panicked: %v\n%s", recovered, stack)
    }),
)
pool.Start()

// Panicking tasks return errors instead of crashing workers.
// The worker stays alive and processes the next task.
resultCh, _ := pool.AddTask(func(ctx context.Context) (interface{}, error) {
    panic("something went wrong")
})
result := <-resultCh
fmt.Println(result.Error) // "task panic: something went wrong"
```

### Pause, Resume, and Status

```go
pool.Pause()

_, err := pool.AddTask(func(ctx context.Context) (interface{}, error) {
    return nil, nil
})
fmt.Println(err) // "worker pool paused"

status := pool.Status()
fmt.Println(status.State) // "paused"

pool.Resume()
// Pool accepts tasks again
```

### Lifecycle Event Observability

```go
events := make(chan routines.Event, 64)
pool, _ := routines.NewWorkerPool(ctx, 4, 16, routines.WithEvents(events))
pool.Start()

go func() {
    for ev := range events {
        fmt.Printf("event: %s\n", ev.Type)
        // event types: pool.started, pool.paused, pool.resumed,
        // pool.draining, pool.stopped, task.started,
        // task.completed, task.panicked
    }
}()
```

### Simple Function Wrappers

```go
done := make(chan struct{})
pool.Go(func() {
    close(done)
})
<-done
```

## Behavior

- **Panic recovery**: Recovered panics become task errors, recorded in `Errors()`, and optionally forwarded to `WithPanicHandler`. Workers survive and continue processing.
- **Non-blocking events**: If the event channel is full, events are dropped. The caller owns the channel and must drain it.
- **Pause semantics**: `Pause()` only stops new submissions. Running and queued tasks continue to completion.
- **Context propagation**: Each task receives a context derived from both the pool context and an optional per-task context. Either cancellation triggers the task context.

## License

See [LICENSE](LICENSE) for details.
