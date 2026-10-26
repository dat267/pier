//go:build unix

package tui

import (
	"os"
	"runtime"
	"sync"
)

// Platform helpers for the terminal port (unix build).

func isDarwin() bool  { return runtime.GOOS == "darwin" }
func isWindows() bool { return runtime.GOOS == "windows" }

// nativeShiftPressed is upstream's isNativeModifierPressed("shift"), which
// needs the native platform helper (C/ObjC). Out of scope (divergence D52);
// native Shift+Enter detection stays disabled.
func nativeShiftPressed() bool { return false }

// resizeWatcher delivers SIGWINCH-driven resize notifications.
//
// stopResizeWatcher signals the watcher and returns without joining it. A
// callback that stops its own watcher is therefore supported (upstream's
// terminal.stop() removes its resize listener and never waits for it), and the
// watcher exits at its next select rather than delivering another callback.
// Joining here deadlocked any host that stopped the terminal from its own
// resize callback. resizeWatcherMu serializes admission, so a restarted watcher
// never coexists with a second one that could still deliver.
var resizeWatcher struct {
	quit chan struct{}
	done chan struct{}
}

var resizeWatcherMu sync.Mutex

func startResizeWatcher(onResize func()) {
	stopResizeWatcher()
	quit := make(chan struct{})
	done := make(chan struct{})
	generation := nextResizeGeneration()
	resizeWatcherMu.Lock()
	resizeWatcher.quit = quit
	resizeWatcher.done = done
	resizeWatcherMu.Unlock()
	sig := make(chan os.Signal, 1)
	signalNotifySIGWINCH(sig)
	go func() {
		defer close(done)
		for {
			select {
			case <-quit:
				signalStopSIGWINCH(sig)
				return
			case <-sig:
				if onResize != nil && resizeGenerationCurrent(generation) {
					onResize()
				}
			}
		}
	}()
}

func stopResizeWatcher() {
	// Invalidate the generation first: a watcher that is mid-callback when this
	// returns cannot deliver another one.
	resizeGeneration.Add(1)
	resizeWatcherMu.Lock()
	quit := resizeWatcher.quit
	resizeWatcher.quit, resizeWatcher.done = nil, nil
	resizeWatcherMu.Unlock()
	if quit != nil {
		close(quit)
	}
}

// resizeWatcherDone reports the running watcher's exit channel (tests).
func resizeWatcherDone() <-chan struct{} {
	resizeWatcherMu.Lock()
	defer resizeWatcherMu.Unlock()
	return resizeWatcher.done
}
