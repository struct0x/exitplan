package exitplan

import (
	"fmt"
	"os"
	"os/signal"
	"time"
)

type opt func(*Exitplan)

// WithExitError sets the callback that will be called when an error occurs during the teardown phase.
// It will be called for every error that occurs during the teardown phase.
func WithExitError(callback func(error)) opt {
	return func(l *Exitplan) {
		l.errorHandler = callback
	}
}

// WithTeardownTimeout sets the timeout for the teardown phase.
func WithTeardownTimeout(timeout time.Duration) opt {
	return func(l *Exitplan) {
		l.teardownTimeout = timeout
	}
}

// WithStartupTimeout sets the timeout for the starting phase.
func WithStartupTimeout(timeout time.Duration) opt {
	return func(l *Exitplan) {
		l.startingTimeout = timeout
	}
}

// WithSignal calls Exitplan.Exit() when the specified signal is received.
func WithSignal(s1 os.Signal, sMany ...os.Signal) opt {
	return func(l *Exitplan) {
		notify := make(chan os.Signal, 1)
		signal.Notify(notify, append([]os.Signal{s1}, sMany...)...)

		go func() {
			sig := <-notify
			l.Exit(fmt.Errorf("%w: %q", ErrSignaled, sig))
		}()
	}
}
