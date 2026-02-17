/*
Package exitplan implements a simple mechanism for managing a lifetime of an application.
It provides a way to register functions that will be called when the application is about to exit.

The application is considered to be running after calling Exitplan.Run() and before calling Exitplan.Exit().
Use Exitplan.Started() to receive a signal when the application enters the running phase.
Use Exitplan.Stopping() to receive a signal when the application enters the teardown phase.
Use Exitplan.Context() to get a context bound to the application lifetime.
Use Exitplan.Completed() to receive a signal when teardown is completed.

The application is considered to be tearing down after calling Exitplan.Exit().
You can use Exitplan.TeardownContext() to get a context that can be used to control the teardown phase.
It is canceled when the teardown timeout is reached.

Ordering of shutdown callbacks
Exitplan executes registered exit callbacks in LIFO order (last registered, first executed).
Async only offloads the execution to a goroutine, but the Exitplan still waits for all callbacks up to the
teardown timeout.
*/
package exitplan

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrSignaled indicates that the application received a termination signal
	ErrSignaled = errors.New("exitplan: received signal")

	// ErrGracefulShutdown indicates that the application was requested to shut down gracefully
	ErrGracefulShutdown = errors.New("exitplan: graceful shutdown requested")

	// ErrStartupTimeout indicates that the application failed to start within the configured timeout
	ErrStartupTimeout = errors.New("exitplan: startup phase timeout exceeded")
)

type Exitplan struct {
	phase atomic.Int32
	die   chan struct{}

	runningCtx    context.Context
	runningCancel context.CancelCauseFunc

	started         chan struct{}
	startingTimeout time.Duration

	teardownCtx     context.Context
	teardownCancel  context.CancelFunc
	teardownTimeout time.Duration

	callbacks []*callback

	panicValue any
	panicOnce  sync.Once
	ranViaRun  bool

	errorHandler func(error)
	callbacksMu  *sync.Mutex
}

// New creates a new instance of Exitplan. It will start in the starting phase.
// Use the With* options to configure it.
func New(opts ...opt) *Exitplan {
	runningCtx, runningCancel := context.WithCancelCause(context.Background())
	teardownCtx, teardownCancel := context.WithCancel(context.Background())
	l := &Exitplan{
		die:         make(chan struct{}),
		started:     make(chan struct{}),
		callbacksMu: &sync.Mutex{},
		callbacks:   make([]*callback, 0),

		runningCtx:    runningCtx,
		runningCancel: runningCancel,

		teardownCtx:    teardownCtx,
		teardownCancel: teardownCancel,
	}
	for _, opt := range opts {
		opt(l)
	}

	l.start()

	return l
}

func (l *Exitplan) start() {
	l.phase.Store(int32(phaseStarting))

	if l.startingTimeout > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), l.startingTimeout)
		go func() {
			defer cancel()
			<-ctx.Done()

			if l.phase.CompareAndSwap(int32(phaseStarting), int32(phaseTeardown)) {
				l.runningCancel(ErrStartupTimeout)
				close(l.die)
			}
		}()
	}

	go func() {
		<-l.die

		go func() {
			if l.teardownTimeout > 0 {
				<-time.After(l.teardownTimeout)
				l.teardownCancel()
			}
		}()

		defer l.teardownCancel()
		defer func() {
			if r := recover(); r != nil {
				l.capturePanic(r)
			}
		}()

		l.exit()
	}()
}

// Started returns a channel that is closed after Run is called and the application enters the running phase.
// It will never fire if Exit is called before Run, or if the startup timeout expires.
func (l *Exitplan) Started() <-chan struct{} {
	return l.started
}

// Completed returns a channel closed after either all callback or teardown context is completed.
// This signal means that now the application can exit.
func (l *Exitplan) Completed() <-chan struct{} {
	return l.teardownCtx.Done()
}

// Stopping returns a channel closed when the teardown phase starts.
// This is equivalent to calling Exitplan.Context().Done().
// It's fired before the application enters the teardown phase by calling Exit.
func (l *Exitplan) Stopping() <-chan struct{} {
	return l.runningCtx.Done()
}

// Context returns a main context. It will be canceled when the application is about to exit.
// It can be used to control the lifetime of the application (via Exit(), signal, or startup timeout).
func (l *Exitplan) Context() context.Context {
	return l.runningCtx
}

// TeardownContext returns a teardown context. It will be canceled after the teardown timeout.
// It can be used to control the shutdown of the application.
// This context is the same as the one passed to the callbacks registered with OnExit* methods.
func (l *Exitplan) TeardownContext() context.Context {
	return l.teardownCtx
}

// OnExit registers a callback that will be called when the application is about to exit.
// Has no effect after calling Exitplan.Run() or Exitplan.Exit().
// Use exitplan.Async option to execute the callback in a separate goroutine.
// exitplan.PanicOnError has no effect on this function.
// See also: OnExitWithError, OnExitWithContext, OnExitWithContextError.
func (l *Exitplan) OnExit(callback func(), exitOpts ...exitCallbackOpt) {
	l.addCallback(func(ctx context.Context) error {
		return callbackWithContext(ctx, func() error {
			callback()
			return nil
		})
	}, exitOpts...)
}

// OnExitWithError registers a callback that will be called when the application is about to exit.
// Has no effect after calling Exitplan.Run() or Exitplan.Exit().
// The callback can return an error that will be passed to the error handler.
// Use exitplan.Async option to execute the callback in a separate goroutine.
// Use exitplan.PanicOnError to panic with the error returned by the callback.
func (l *Exitplan) OnExitWithError(callback func() error, exitOpts ...exitCallbackOpt) {
	l.addCallback(func(ctx context.Context) error {
		return callbackWithContext(ctx, callback)
	}, exitOpts...)
}

// OnExitWithContext registers a callback that will be called when the application is about to exit.
// Has no effect after calling Exitplan.Run() or Exitplan.Exit().
// The callback will receive a context that will be canceled after the teardown timeout.
// Use exitplan.Async option to execute the callback in a separate goroutine.
// exitplan.PanicOnError has no effect on this function.
func (l *Exitplan) OnExitWithContext(callback func(context.Context), exitOpts ...exitCallbackOpt) {
	l.addCallback(func(ctx context.Context) error {
		callback(ctx)
		return nil
	}, exitOpts...)
}

// OnExitWithContextError registers a callback that will be called when the application is about to exit.
// Has no effect after calling Exitplan.Run() or Exitplan.Exit().
// The callback will receive a context that will be canceled after the teardown timeout.
// The callback can return an error that will be passed to the error handler.
// Use exitplan.Async option to execute the callback in a separate goroutine.
// Use exitplan.PanicOnError to panic with the error returned by the callback.
func (l *Exitplan) OnExitWithContextError(callback func(context.Context) error, exitOpts ...exitCallbackOpt) {
	l.addCallback(callback, exitOpts...)
}

func (l *Exitplan) addCallback(cb func(context.Context) error, exitOpts ...exitCallbackOpt) {
	c := &callback{
		name: callerLocation(3),
		fn:   cb,
	}

	for _, opt := range exitOpts {
		opt(c)
	}

	l.callbacksMu.Lock()
	defer l.callbacksMu.Unlock()

	if l.phase.Load() != int32(phaseStarting) {
		return
	}

	l.callbacks = append(l.callbacks, c)
}

// Exit stops the application and waits for all registered callbacks to complete.
// It returns the provided reason, enabling the pattern: return lc.Exit(err).
// If the reason is nil, ErrGracefulShutdown will be used.
// Exit can be called before or after Run.
// Multiple calls to Exit are safe; all will block until teardown completes.
func (l *Exitplan) Exit(reason error) error {
	if reason == nil {
		reason = ErrGracefulShutdown
	}

	if l.phase.CompareAndSwap(int32(phaseRunning), int32(phaseTeardown)) ||
		l.phase.CompareAndSwap(int32(phaseStarting), int32(phaseTeardown)) {
		l.runningCancel(reason)
		close(l.die)
	}

	<-l.teardownCtx.Done()

	if !l.ranViaRun && l.panicValue != nil {
		panic(l.panicValue)
	}

	return context.Cause(l.runningCtx)
}

// Run starts the application. It will block until the application is stopped by calling Exit.
// It will also block until all the registered callbacks are executed.
// If the teardown timeout is set, it will be used to cancel the context passed to the callbacks.
// If Exit was called before Run, Run will wait for teardown to complete and return the exit cause.
// Returns the error that caused the application to stop.
func (l *Exitplan) Run() (exitCause error) {
	l.ranViaRun = true

	// If Exit() was already called, skip the starting→running transition.
	if l.phase.CompareAndSwap(int32(phaseStarting), int32(phaseRunning)) {
		close(l.started)
	}

	<-l.teardownCtx.Done()

	if l.panicValue != nil {
		panic(l.panicValue)
	}

	return context.Cause(l.runningCtx)
}

func (l *Exitplan) exit() {
	ctx := l.TeardownContext()

	l.callbacksMu.Lock()
	callbacks := make([]*callback, len(l.callbacks))
	copy(callbacks, l.callbacks)
	l.callbacksMu.Unlock()

	wg := sync.WaitGroup{}

	for _, cb := range callbacks {
		if cb.executeBehaviour != executeAsync {
			continue
		}

		wg.Add(1)
		go func(cb *callback) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					l.capturePanic(r)
				}
			}()

			execCtx := ctx
			if cb.timeout > 0 {
				var cancel context.CancelFunc
				execCtx, cancel = context.WithTimeout(ctx, cb.timeout)
				defer cancel()
			}

			if err := cb.fn(execCtx); err != nil {
				l.handleExitError(cb.errorBehaviour, &CallbackErr{
					Name: cb.name,
					Err:  err,
				})
			}
		}(cb)
	}

	for i := len(callbacks) - 1; i >= 0; i-- {
		cb := callbacks[i]
		if cb.executeBehaviour != executeSync {
			continue
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		var cancel context.CancelFunc = func() {}
		execCtx := ctx
		if cb.timeout > 0 {
			execCtx, cancel = context.WithTimeout(ctx, cb.timeout)
		}

		if err := cb.fn(execCtx); err != nil {
			l.handleExitError(cb.errorBehaviour, &CallbackErr{
				Name: cb.name,
				Err:  err,
			})
		}

		cancel()
	}

	wg.Wait()
}

func (l *Exitplan) handleExitError(errBehaviour exitBehaviour, err error) {
	if l.errorHandler != nil {
		l.errorHandler(err)
	}

	if errBehaviour == panicOnError {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return // ignore context errors
		}
		if err != nil {
			panic(err)
		}
	}
}

func (l *Exitplan) capturePanic(r any) {
	l.panicOnce.Do(func() {
		l.panicValue = r
	})
}

func callbackWithContext(ctx context.Context, callback func() error) error {
	errChan := make(chan error)
	go func() {
		errChan <- callback()
	}()
	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
