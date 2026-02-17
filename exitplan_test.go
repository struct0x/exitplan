package exitplan_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/struct0x/exitplan"
)

var errUnexpected = fmt.Errorf("unexpected err")

func TestExitCallbacks(t *testing.T) {
	t.Parallel()

	callsMutex := &sync.Mutex{}
	calls := make([]string, 0, 5)
	asyncCalled := false

	l := exitplan.New()

	l.OnExit(func() {
		callsMutex.Lock()
		defer callsMutex.Unlock()
		calls = append(calls, "exit")
	})

	l.OnExitWithError(func() error {
		callsMutex.Lock()
		defer callsMutex.Unlock()
		calls = append(calls, "exit with error")
		return nil
	})

	l.OnExitWithContext(func(ctx context.Context) {
		callsMutex.Lock()
		defer callsMutex.Unlock()
		calls = append(calls, "exit with context")
	})

	l.OnExitWithContextError(func(ctx context.Context) error {
		callsMutex.Lock()
		defer callsMutex.Unlock()
		calls = append(calls, "exit with context and error")
		return nil
	})

	l.OnExitWithContextError(func(ctx context.Context) error {
		callsMutex.Lock()
		defer callsMutex.Unlock()
		asyncCalled = true
		return nil
	}, exitplan.Async)

	go func() {
		<-l.Started()

		l.OnExit(func() {
			t.Error("this should never be called")
		})

		_ = l.Exit(errUnexpected)
	}()

	if err := l.Run(); !errors.Is(err, errUnexpected) {
		t.Errorf("expected %q, got: %q", errUnexpected, err)
	}

	expected := []string{"exit with context and error", "exit with context", "exit with error", "exit"}
	if slices.Compare(calls, expected) != 0 {
		t.Errorf("expected 5 calls, got %v", calls)
	}

	if !asyncCalled {
		t.Error("async callback was not called")
	}
}

func TestStartupTimeout(t *testing.T) {
	t.Parallel()

	l := exitplan.New(exitplan.WithStartupTimeout(10 * time.Millisecond))

	var called bool
	l.OnExit(func() {
		called = true
	})

	// running expensive setup
	time.Sleep(20 * time.Millisecond)

	err := l.Run()
	if !errors.Is(err, exitplan.ErrStartupTimeout) {
		t.Errorf("expected %q, got: %q", exitplan.ErrStartupTimeout, err)
	}

	if !called {
		t.Error("callback was not called")
	}
}

func TestPanic(t *testing.T) {
	t.Parallel()

	t.Run("panic_in_run", func(t *testing.T) {
		l := exitplan.New()

		defer func() {
			if r := recover(); r == nil {
				t.Errorf("The code did not panic")
			}
		}()

		l.OnExitWithContextError(func(ctx context.Context) error {
			return errors.New("test error")
		}, exitplan.PanicOnError)

		go func() {
			<-l.Started()
			_ = l.Exit(errUnexpected)
		}()

		if err := l.Run(); !errors.Is(err, errUnexpected) {
			t.Errorf("expected %q, got: %q", errUnexpected, err)
		}

		t.Error("The code did not panic")
	})

	t.Run("panic_in_exit", func(t *testing.T) {
		l := exitplan.New()

		defer func() {
			if r := recover(); r == nil {
				t.Errorf("The code did not panic")
			}
		}()

		l.OnExitWithContextError(func(ctx context.Context) error {
			return errors.New("test error")
		}, exitplan.PanicOnError)

		if err := l.Exit(errUnexpected); !errors.Is(err, errUnexpected) {
			t.Errorf("expected %q, got: %q", errUnexpected, err)
		}

		t.Error("The code did not panic")
	})

	t.Run("async_panic", func(t *testing.T) {
		l := exitplan.New()

		defer func() {
			if r := recover(); r == nil {
				t.Errorf("The code did not panic")
			}
		}()

		l.OnExitWithContextError(func(ctx context.Context) error {
			return errors.New("test error")
		}, exitplan.Async, exitplan.PanicOnError)

		go func() {
			<-l.Started()
			_ = l.Exit(errUnexpected)
		}()

		if err := l.Run(); !errors.Is(err, errUnexpected) {
			t.Errorf("expected %q, got: %q", errUnexpected, err)
		}

		t.Error("The code did not panic")
	})
}

func TestTeardownTimeout(t *testing.T) {
	t.Parallel()

	timeout := 10 * time.Millisecond
	timeoutJitter := 5 * time.Millisecond

	l := exitplan.New(exitplan.WithTeardownTimeout(timeout))

	l.OnExit(func() {
		panic("should not be called")
	})

	l.OnExit(func() {
		time.Sleep(2 * timeout)
	})

	go func() {
		<-l.Started()
		_ = l.Exit(errUnexpected)
	}()

	start := time.Now()
	if err := l.Run(); !errors.Is(err, errUnexpected) {
		t.Errorf("expected %q, got: %q", errUnexpected, err)
	}
	end := time.Now()

	if end.Sub(start) < timeout-timeoutJitter || end.Sub(start) > timeout+timeoutJitter {
		t.Errorf("expected timeout between %v and %v, got %v", timeout-timeoutJitter, timeout+timeoutJitter, end.Sub(start))
	}
}

func TestOnExitTimeout(t *testing.T) {
	t.Parallel()

	timeout := 10 * time.Millisecond
	timeoutJitter := 5 * time.Millisecond

	l := exitplan.New()

	called := atomic.Bool{}
	l.OnExit(func() {
		time.Sleep(2 * timeout)
		called.Store(true)
	}, exitplan.Timeout(timeout))

	go func() {
		<-l.Started()
		_ = l.Exit(errUnexpected)
	}()

	start := time.Now()
	if err := l.Run(); !errors.Is(err, errUnexpected) {
		t.Errorf("expected %q, got: %q", errUnexpected, err)
	}
	end := time.Now()

	if end.Sub(start) < timeout-timeoutJitter || end.Sub(start) > timeout+timeoutJitter {
		t.Errorf("expected timeout between %v and %v, got %v", timeout-timeoutJitter, timeout+timeoutJitter, end.Sub(start))
	}

	if called.Load() {
		t.Error("callback was called")
	}
}

func TestCallbackName(t *testing.T) {
	t.Parallel()

	mu := sync.Mutex{}
	names := make([]string, 0)

	l := exitplan.New(
		exitplan.WithExitError(func(err error) {
			var exErr *exitplan.CallbackErr
			if errors.As(err, &exErr) {
				mu.Lock()
				names = append(names, exErr.Name)
				mu.Unlock()
			}
		}),
	)

	l.OnExitWithContextError(func(ctx context.Context) error {
		return errors.New("test error")
	}, exitplan.Name("cb1"), exitplan.Async)

	l.OnExitWithContextError(func(ctx context.Context) error {
		return errors.New("test error")
	}, exitplan.Name("cb2"))

	go func() {
		<-l.Started()
		_ = l.Exit(errUnexpected)
	}()

	if err := l.Run(); !errors.Is(err, errUnexpected) {
		t.Errorf("expected %q, got: %q", errUnexpected, err)
	}

	if len(names) != 2 {
		t.Errorf("expected 2 callback calls got %d", len(names))
	}

	if !reflect.DeepEqual(names, []string{"cb2", "cb1"}) {
		t.Errorf("expected names to have cb1 callback, got: %v", names)
	}
}

func TestShortCircuitExit(t *testing.T) {
	t.Parallel()

	callsMutex := &sync.Mutex{}
	calls := make([]string, 0, 5)
	asyncCalled := false

	run := func() error {
		l := exitplan.New()

		l.OnExit(func() {
			callsMutex.Lock()
			defer callsMutex.Unlock()
			calls = append(calls, "exit")
		})

		l.OnExitWithError(func() error {
			callsMutex.Lock()
			defer callsMutex.Unlock()
			calls = append(calls, "exit with error")
			return nil
		})

		l.OnExitWithContext(func(ctx context.Context) {
			callsMutex.Lock()
			defer callsMutex.Unlock()
			calls = append(calls, "exit with context")
		})

		l.OnExitWithContextError(func(ctx context.Context) error {
			callsMutex.Lock()
			defer callsMutex.Unlock()
			calls = append(calls, "exit with context and error")
			return nil
		})

		l.OnExitWithContextError(func(ctx context.Context) error {
			callsMutex.Lock()
			defer callsMutex.Unlock()
			asyncCalled = true
			return nil
		}, exitplan.Async)

		return l.Exit(errUnexpected)
	}

	err := run()
	if !errors.Is(err, errUnexpected) {
		t.Fatalf("expected %q, got: %q", errUnexpected, err)
	}

	expected := []string{"exit with context and error", "exit with context", "exit with error", "exit"}
	if slices.Compare(calls, expected) != 0 {
		t.Errorf("expected 5 calls, got %v", calls)
	}

	if !asyncCalled {
		t.Error("async callback was not called")
	}
}

func TestStoppingSignal(t *testing.T) {
	t.Parallel()

	t.Run("fires_after_exit", func(t *testing.T) {
		t.Parallel()

		l := exitplan.New()
		signals := make([]string, 0, 3)
		done := make(chan struct{})

		go func() {
			defer close(done)

			<-l.Started()
			signals = append(signals, "started")

			<-l.Stopping()
			signals = append(signals, "stopping")

			<-l.Completed()
			signals = append(signals, "completed")
		}()

		go func() {
			<-l.Started()
			_ = l.Exit(errUnexpected)
		}()

		if err := l.Run(); !errors.Is(err, errUnexpected) {
			t.Errorf("expected %q, got: %q", errUnexpected, err)
		}

		<-done

		expected := []string{"started", "stopping", "completed"}
		if !slices.Equal(signals, expected) {
			t.Errorf("expected signals %v, got %v", expected, signals)
		}
	})

	t.Run("fires_on_startup_timeout", func(t *testing.T) {
		t.Parallel()

		l := exitplan.New(exitplan.WithStartupTimeout(10 * time.Millisecond))

		time.Sleep(20 * time.Millisecond)

		select {
		case <-l.Started():
			t.Error("Started() should not fire on startup timeout")
		default:
		}

		select {
		case <-l.Stopping():
		default:
			t.Error("Stopping() should fire on startup timeout")
		}
	})

	t.Run("fires_on_early_exit", func(t *testing.T) {
		t.Parallel()

		l := exitplan.New()

		select {
		case <-l.Started():
			t.Error("Started() should not fire before Run()")
		default:
		}

		_ = l.Exit(errUnexpected)

		select {
		case <-l.Started():
			t.Error("Started() should not fire after early Exit()")
		default:
		}

		select {
		case <-l.Stopping():
		default:
			t.Error("Stopping() should fire after early Exit()")
		}

		select {
		case <-l.Completed():
		default:
			t.Error("Completed() should fire after early Exit()")
		}
	})
}

func TestRegistersAfterExit(t *testing.T) {
	t.Parallel()

	callsMutex := &sync.Mutex{}
	calls := make([]string, 0, 5)

	l := exitplan.New()

	l.OnExit(func() {
		callsMutex.Lock()
		defer callsMutex.Unlock()
		calls = append(calls, "exit")
	})

	exitErr := l.Exit(nil)

	l.OnExitWithContextError(func(ctx context.Context) error {
		callsMutex.Lock()
		defer callsMutex.Unlock()
		calls = append(calls, "exit with context and error")
		return nil
	})

	runErr := l.Run()

	expected := []string{"exit"}
	if slices.Compare(calls, expected) != 0 {
		t.Errorf("expected 5 calls, got %v", calls)
	}

	if !errors.Is(exitErr, runErr) {
		t.Errorf("expected %q, got: %q", exitErr, runErr)
	}
}
