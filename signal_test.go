//go:build !windows

package exitplan_test

import (
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/struct0x/exitplan"
)

func TestSignalHandling(t *testing.T) {
	t.Parallel()

	ex := exitplan.New(
		exitplan.WithSignal(syscall.SIGUSR1),
	)

	callbackRan := false
	ex.OnExit(func() {
		callbackRan = true
	})

	done := make(chan error, 1)
	go func() {
		done <- ex.Run()
	}()

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGUSR1); err != nil {
		t.Errorf("error calling Kill: %v", err)
	}

	select {
	case err := <-done:
		if !errors.Is(err, exitplan.ErrSignaled) {
			t.Errorf("expected ErrSignaled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for shutdown")
	}

	if !callbackRan {
		t.Error("callback should have run")
	}
}
