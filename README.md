# routines-go

A Go library that provides helper functions to manage and have control over goroutines in a generic, useful, and stable way.

## Features

- Launch goroutines with or without context
- Handle task results and errors through channels
- Automatic panic recovery
- Wait for all goroutines to complete
- Memory monitoring utilities
- Worker pool with task queue for controlled concurrency

## Installation

```bash
go get github.com/pixie-sh/routines-go
```

## Usage

### Basic Usage

```go
import (
    "github.com/pixie-sh/routines-go/routines"
)

// Launch a simple goroutine with no return value
routines.Go(func() {
    // Your code here
})

// Launch a goroutine with context
ctx := context.Background()
routines.GoCtx(ctx, func() {
    // Your code here
})

// Wait for all goroutines to complete
routines.WaitUntil()
```

### Tasks with Return Values and Error Handling

```go
// Launch a task that returns a value and possibly an error
resultChan, errChan := routines.GoTask(func(ctx context.Context) (any, error) {
    // Your code here
    return "result", nil
})

// Handle the result or error
select {
case result := <-resultChan:
    fmt.Println("Result:", result)
case err := <-errChan:
    fmt.Println("Error:", err)
}
```

### Memory Monitoring

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

// Start memory monitoring in a goroutine
routines.Go(func() {
    routines.StartMemoryMonitoring(ctx, 5, logger.Logger) // Log memory stats every 5 seconds
})
```

## Custom Routines Pool

```go
// Create a custom routines pool with a specific context
ctx := context.Background()
pool := routines.NewRoutinesPool(ctx)

// Use the pool to launch goroutines
pool.Go(func() {
    // Your code here
})

// Wait for all goroutines in this pool to complete
pool.WaitUntil()
```

## Worker Pool with Task Queue

```go
// Create a worker pool with 5 workers and a queue size of 10
ctx := context.Background()
pool, err := routines.NewWorkerPool(ctx, 5, 10)
if err != nil {
    log.Fatalf("Failed to create worker pool: %v", err)
}

// Start the worker pool
if err := pool.Start(); err != nil {
    log.Fatalf("Failed to start worker pool: %v", err)
}
defer pool.Stop() // Stop the pool when done

// Submit tasks to the pool
for i := 0; i < 20; i++ {
    taskID := i // Capture the loop variable
    err := pool.Submit(func(ctx context.Context) (any, error) {
        // Process task
        fmt.Printf("Processing task %d\n", taskID)
        return nil, nil
    })
    if err != nil {
        log.Printf("Failed to submit task %d: %v", i, err)
    }
}

// Wait for all submitted tasks to complete
pool.Wait()

// Submit a task and wait for its result
result, err := pool.SubmitWait(func(ctx context.Context) (any, error) {
    // Process task and return a result
    return "task completed", nil
})
if err != nil {
    log.Printf("Task failed: %v", err)
} else {
    fmt.Printf("Task result: %v\n", result)
}
```
