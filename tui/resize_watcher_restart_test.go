//go:build !windows

package tui

import (
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// Reference: pi v1.4.0 (4a080252), packages/tui/src/terminal.ts stop removes its
// SIGWINCH listener and never waits for a callback, so a host may stop the
// terminal from inside its own resize callback. Joining the watcher there
// deadlocked such a host (CR-063).
func TestStopInsideResizeCallbackDoesNotDeadlock(t *testing.T) {
	terminal := NewProcessTerminal(os.Stdin, os.Stdin)
	var width atomic.Int64
	width.Store(80)
	// A real size change is what makes Start's watcher call onResize.
	terminal.sizeFn = func() (int, int) { return int(width.Load()), 24 }

	stopped := make(chan struct{})
	terminal.Start(func(string) {}, func() {
		terminal.Stop()
		close(stopped)
	})
	defer terminal.Stop()

	width.Store(100)
	raiseSIGWINCH(t)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop from the resize callback deadlocked")
	}
	// The watcher is signalled rather than joined, so its exit is observable
	// through its done channel.
	if done := resizeWatcherDone(); done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("the stopped resize watcher did not exit")
		}
	}
}

// A restart admits exactly one watcher: a superseded one may still be finishing
// a callback, but it never delivers again.
func TestResizeWatcherRestartDeliversOnce(t *testing.T) {
	first := make(chan struct{}, 4)
	second := make(chan struct{}, 4)
	release := make(chan struct{})
	startResizeWatcher(func() {
		first <- struct{}{}
		<-release
	})

	raiseSIGWINCH(t)
	select {
	case <-first:
	case <-time.After(5 * time.Second):
		t.Fatal("the first watcher never delivered")
	}
	// Replace the watcher while its callback is still blocked.
	startResizeWatcher(func() { second <- struct{}{} })
	close(release)
	raiseSIGWINCH(t)

	select {
	case <-second:
	case <-time.After(5 * time.Second):
		t.Fatal("the replacement watcher never delivered")
	}
	select {
	case <-first:
		t.Fatal("the superseded watcher delivered after being replaced")
	case <-time.After(100 * time.Millisecond):
	}
	stopResizeWatcher()
}

func raiseSIGWINCH(t *testing.T) {
	t.Helper()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatalf("raise SIGWINCH: %v", err)
	}
}
