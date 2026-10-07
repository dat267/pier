package interactive

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dat267/pier/tui"
)

// TestStopModeSwitchesToRegularOnTranscriptExit covers the fullscreen exit
// replay: with fullscreenExitOutput="transcript" the teardown swaps the
// renderer to the regular main screen (its final frame stays in the terminal
// scrollback) before the terminal is restored.
func TestStopModeSwitchesToRegularOnTranscriptExit(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	app.Init(context.Background())
	screen, ok := app.lifecycle.CurrentUI().(*tui.AltScreen)
	if !ok {
		t.Fatalf("initial UI = %T", app.lifecycle.CurrentUI())
	}
	screen.Start()
	screen.DisableAutoRender()

	app.StopMode("transcript")

	current, ok := app.lifecycle.CurrentUI().(*tui.MainScreen)
	if !ok {
		t.Fatalf("after StopMode the renderer = %T, want MainScreen", app.lifecycle.CurrentUI())
	}
	_ = current
	// And with "resume-hint" the alt screen is kept (only the screen state is
	// restored, no replay).
}

func TestStopModeKeepsAltScreenOnResumeHintExit(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	app.Init(context.Background())
	screen, ok := app.lifecycle.CurrentUI().(*tui.AltScreen)
	if !ok {
		t.Fatalf("initial UI = %T", app.lifecycle.CurrentUI())
	}
	screen.Start()
	screen.DisableAutoRender()

	app.StopMode("resume-hint")

	if _, ok := app.lifecycle.CurrentUI().(*tui.AltScreen); !ok {
		t.Fatalf("after StopMode the renderer = %T, want AltScreen", app.lifecycle.CurrentUI())
	}
}

// D199: an optional worker that ignores cancellation must not prevent the
// real StopMode teardown. The worker is released only after observing exit.
func TestStopModeDoesNotWaitForBlockedPrerender(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	app.Init(context.Background())
	queue := app.transcript.PrerenderQueue
	release, started := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer queue.Flush()
	defer unblock()
	queue.Go(func() { close(started); <-release })
	<-started
	stopped := make(chan struct{})
	go func() { app.StopMode("resume-hint"); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		unblock()
		<-stopped
		t.Fatal("StopMode waited for optional pre-render work")
	}
}

// D199: normal run-context cancellation also reaches Close, without necessarily
// going through StopMode. That path must close the optional queues too.
func TestCloseRejectsLateOptionalWork(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	app.Init(context.Background())
	app.Close()
	var ran atomic.Bool
	app.prerenderQueue.Go(func() { ran.Store(true) })
	app.prerenderQueue.Flush()
	if ran.Load() {
		t.Fatal("Close left the optional queue accepting work")
	}
}

// TestStopModeStopsTheAppOffloopQueues pins that StopMode stops the queues the
// app registered (threaded through its offloop group), not just the settings
// and session queues it flushes explicitly — the theme queue used to be left
// running.
func TestStopModeStopsTheAppOffloopQueues(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	app.Init(context.Background())
	queue := app.transcript.PrerenderQueue
	if queue == nil {
		t.Fatal("pre-render queue not wired")
	}

	app.StopMode("transcript")

	var ran atomic.Bool
	queue.Go(func() { ran.Store(true) })
	queue.Flush()
	if ran.Load() {
		t.Fatal("StopMode did not stop the app's off-loop queues")
	}
}
