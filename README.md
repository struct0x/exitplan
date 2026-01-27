# Exitplan

[![Go Reference](https://pkg.go.dev/badge/github.com/struct0x/exitplan.svg)](https://pkg.go.dev/github.com/struct0x/exitplan)
[![Go Report Card](https://goreportcard.com/badge/github.com/struct0x/exitplan)](https://goreportcard.com/report/github.com/struct0x/exitplan)
![Coverage](https://img.shields.io/badge/Coverage-90.9%25-brightgreen)

A Go library for managing the lifecycle of an application with graceful shutdown capabilities.

## Overview

The Exitplan library provides a simple mechanism for managing the lifetime of an application.
It helps you handle application running, and shutdown phases with proper resource cleanup.

Key features include:

- Distinct application lifecycle phases (running, teardown)
- Context-based lifecycle management
- Graceful shutdown with customizable timeout
- Flexible callback registration for cleanup operations
- Signal handling for clean application termination
- Synchronous and asynchronous shutdown callbacks
- Error handling during shutdown

## Installation

```bash
go get github.com/struct0x/exitplan
```

## Lifecycle Phases

Exitplan manages two lifecycle phases:

- **Running**: active between `Run()` and `Exit()`. Use
  `Context()` for workers and other long-running tasks.  
  It is canceled as soon as shutdown begins (via `Exit()`, signal, or startup timeout).

- **Teardown**: after `Exit()` is called. Use `TeardownContext()` in shutdown callbacks.  
  It is canceled when the global teardown timeout elapses.

Use
`Started()` to receive a signal when the application enters the running phase.                                                                                                                                                                                                                                                                                                                                             
This is useful for readiness probes or coordinating dependent services.

### Startup Timeout

Use `WithStartupTimeout()` to detect stuck initialization:

  ```go      
package main

import (
	"time"

	"github.com/struct0x/exitplan"
)

func main() {
	_ = exitplan.New(
		exitplan.WithStartupTimeout(10 * time.Second),
	)

	// If Run() isn't called within 10 seconds,
	// Context() is canceled and teardown begins
}

  ```

This is useful when initialization depends on external services that might hang.

### Callback ordering

Shutdown callbacks registered with `OnExit*` are executed in **LIFO order
** (last registered, first executed).  
This mirrors resource lifecycles: if you start DB then HTTP, shutdown runs HTTP then DB.  
Callbacks marked with `Async` are awaited up to the teardown timeout.

## Usage

### Basic Example

```go
package main

import (
	"fmt"
	"syscall"
	"time"

	"github.com/struct0x/exitplan"
)

func main() {
	// Create a new Exitplan instance with signal handling for graceful shutdown
	ex := exitplan.New(
		exitplan.WithSignal(syscall.SIGINT, syscall.SIGTERM),
		exitplan.WithTeardownTimeout(5*time.Second),
	)

	// Register cleanup functions
	ex.OnExit(func() {
		fmt.Println("Cleaning up resources...")
		time.Sleep(1 * time.Second)
		fmt.Println("Cleanup complete")
	})

	// Start your application
	fmt.Println("Application starting...")

	// Run the application (blocks until Exit() is called)
	exitCause := ex.Run()
	fmt.Printf("Application exited: %v\n", exitCause)
}

```

### Advanced Example with Context

```go
package main

import (
	"context"
	"fmt"
	"syscall"
	"time"

	"github.com/struct0x/exitplan"
)

func main() {
	// Create a new Exitplan instance with options
	ex := exitplan.New(
		exitplan.WithSignal(syscall.SIGINT, syscall.SIGTERM),
		exitplan.WithTeardownTimeout(10*time.Second),
		exitplan.WithExitError(func(err error) {
			fmt.Printf("Error during shutdown: %v\n", err)
		}),
	)

	// Signal readiness when Run() starts
	go func() {
		<-ex.Started()
		fmt.Println("Application is now running and ready")
		// e.g., signal readiness probe, notify dependent services
	}()

	// Initialize resources before Run()
	// Use context.WithTimeout() if you need bounded initialization
	// ctx, cancel := context.WithTimeout(ex.Context(), 5*time.Second)	
	// defer cancel()
	// err := db.Ping(ctx)

	// Register cleanup with context awareness
	ex.OnExitWithContext(func(ctx context.Context) {
		fmt.Println("Starting cleanup...")

		select {
		case <-time.After(2 * time.Second):
			fmt.Println("Cleanup completed successfully")
		case <-ctx.Done():
			fmt.Println("Cleanup was interrupted by timeout")
		}
	})

	// Register cleanup that might return an error
	ex.OnExitWithContextError(func(ctx context.Context) error {
		fmt.Println("Closing database connection...")
		time.Sleep(1 * time.Second)
		return nil
	})

	// Register an async cleanup task
	ex.OnExit(func() {
		fmt.Println("Performing async cleanup...")
		time.Sleep(3 * time.Second)
		fmt.Println("Async cleanup complete")
	}, exitplan.Async)

	// Start your application
	fmt.Println("Application starting...")

	// Get the running context to use in your application
	ctx := ex.Context()

	// Start a worker that respects the application lifecycle
	workerDone := make(chan struct{})
	go func() {
		for {
			select {
			case <-ctx.Done():
				fmt.Println("Worker shutting down...")
				time.Sleep(100 * time.Millisecond) // Simulate some teardown work
				close(workerDone)
				return
			case <-time.After(1 * time.Second):
				fmt.Println("Worker doing work...")
			}
		}
	}()
	ex.OnExitWithContext(func(ctx context.Context) {
		select {
		case <-workerDone:
			fmt.Println("Worker shutdown complete")
		case <-ctx.Done():
			fmt.Println("Worker shutdown interrupted")
		}
	})

	// Run the application (blocks until Exit() is called)
	exitCause := ex.Run()
	fmt.Printf("Application exited: %v\n", exitCause)
}

```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
