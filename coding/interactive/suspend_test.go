//go:build !windows

package interactive

import (
	"errors"
	"runtime"
	"testing"
	"time"
)

// pi v1.5.2 (b4afdddf) modes/interactive/interactive-mode.ts handleCtrlZ wraps the
// SIGTSTP send in try/catch and clears its keep-alive timer plus its SIGINT
// listener on failure. The port stopped the SIGCONT subscription but left its
// waiter blocked on a channel that can no longer receive, leaking that goroutine
// for the life of the process.
func TestSuspendReleasesItsWaiterWhenTheSignalFails(t *testing.T) {
	previous := suspendProcessGroup
	t.Cleanup(func() { suspendProcessGroup = previous })
	suspendProcessGroup = func() error { return errors.New("cannot suspend") }

	before := runtime.NumGoroutine()
	for i := 0; i < 16; i++ {
		suspendProcess(nil)
	}
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before+2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if live := runtime.NumGoroutine(); live > before+2 {
		t.Fatalf("%d goroutines still running after 16 failed suspends (was %d)", live, before)
	}
}
