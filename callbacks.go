package exitplan

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"time"
)

type CallbackErr struct {
	Name string
	Err  error
}

func (e *CallbackErr) Error() string {
	return fmt.Sprintf("callback %s: %v", e.Name, e.Err)
}

func (e *CallbackErr) Unwrap() error {
	return e.Err
}

type exitCallbackOpt func(*callback)

// Async sets the callback to be executed in a separate goroutine.
// Exitplan will wait for all Async callbacks to complete before exiting.
// Use Timeout to bound the callback.
func Async(c *callback) {
	c.executeBehaviour = executeAsync
}

// PanicOnError sets the callback to panic with the error returned by the callback.
func PanicOnError(c *callback) {
	c.errorBehaviour = panicOnError
}

// Timeout sets the timeout for the callback.
func Timeout(timeout time.Duration) exitCallbackOpt {
	return func(c *callback) {
		c.timeout = timeout
	}
}

// Name sets callback name, used for identification.
func Name(name string) exitCallbackOpt {
	return func(c *callback) {
		c.name = name
	}
}

type executeBehaviour int

const (
	executeSync executeBehaviour = iota
	executeAsync
)

type exitBehaviour int

const (
	carryOnWithError exitBehaviour = iota
	panicOnError
)

type callback struct {
	name             string
	executeBehaviour executeBehaviour
	errorBehaviour   exitBehaviour
	timeout          time.Duration
	fn               func(context.Context) error
}

func callerLocation(skip int) string {
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return "unknown"
	}
	return fmt.Sprintf("%s:%d", filepath.Base(file), line)
}
