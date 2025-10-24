package exitplan_test

import (
	"context"
	"errors"
	"fmt"
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
		<-l.StartingContext().Done()
		l.Exit(errUnexpected)
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
		<-l.StartingContext().Done()
		l.Exit(errUnexpected)
	}()

	if err := l.Run(); !errors.Is(err, errUnexpected) {
		t.Errorf("expected %q, got: %q", errUnexpected, err)
	}

	t.Error("The code did not panic")
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
		<-l.StartingContext().Done()
		l.Exit(errUnexpected)
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
		<-l.StartingContext().Done()
		l.Exit(errUnexpected)
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
