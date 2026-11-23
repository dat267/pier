//go:build !windows

package tui

import (
	"os"
	"runtime"
	"strings"
	"sync"
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

// pi v1.5.2 (b4afdddf) packages/tui/src/terminal.ts start() installs one resize
// listener per call, and ProcessTerminal.Start is reachable from several places
// (the SIGCONT resume handler runs on its own goroutine), so two starts can
// overlap. startResizeWatcher used to stop the predecessor and then store its own
// quit channel under separate lock acquisitions: an interleaved pair overwrote
// the first quit channel, which was never closed, and that watcher goroutine
// leaked for the life of the process.
func TestConcurrentResizeWatcherStartsDoNotLeak(t *testing.T) {
	stopResizeWatcher()
	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() {
			startResizeWatcher(func() {})
		})
	}
	wg.Wait()
	// Every superseded watcher must exit once it is stopped or superseded.
	done := resizeWatcherDone()
	stopResizeWatcher()
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("the admitted watcher did not exit")
		}
	}
	// A superseded goroutine leaks until the process exits, which the package's
	// leak checker reports only at the end of the run; count them here so the
	// failure names this test.
	deadline := time.Now().Add(5 * time.Second)
	for liveResizeWatcherGoroutines() > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if live := liveResizeWatcherGoroutines(); live > 0 {
		t.Fatalf("%d superseded resize watcher goroutines still running", live)
	}
}

// liveResizeWatcherGoroutines counts goroutines blocked in the resize watcher's
// select loop.
func liveResizeWatcherGoroutines() int {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	count := 0
	for goroutine := range strings.SplitSeq(string(buf), "\n\n") {
		if strings.Contains(goroutine, "startResizeWatcher.func1") {
			count++
		}
	}
	return count
}

func raiseSIGWINCH(t *testing.T) {
	t.Helper()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatalf("raise SIGWINCH: %v", err)
	}
}
