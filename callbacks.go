package exitplan

import (
	"context"
	"time"
)

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
	executeBehaviour executeBehaviour
	errorBehaviour   exitBehaviour
	timeout          time.Duration
	fn               func(context.Context) error
}
