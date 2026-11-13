package tui

import (
	"testing"
	"time"
)

// A544: the animation walk starts at the mounted roots, and the alt screen is
// the renderer rather than a child, so its own AnimationFrame (flash deadline
// and selection auto-scroll) was never evaluated. Upstream's TuiBase owns the
// timers for those two, so the screen has to take part in the walk.
//
// TestFlashingScreenAsksForAFrameAtTheFlashExpiry calls AnimationFrame directly,
// which is why the missing wiring went unnoticed: a flash stayed on an idle
// screen until some unrelated repaint, and a held drag at the viewport edge
// never scrolled.
func TestNextAnimationReachesTheAltScreen(t *testing.T) {
	screen := newFlashTestScreen(t)
	defer screen.Stop(TuiStopOptions{})

	if want, _ := screen.NextAnimation(); want {
		t.Fatal("an idle alt screen asked for an animation frame")
	}

	screen.Flash("Copied!")
	want, delay := screen.NextAnimation()
	if !want {
		t.Fatal("a flash was pending and the walk asked for no frame: nothing wakes at its expiry")
	}
	if delay <= 0 || delay > 1100*time.Millisecond {
		t.Fatalf("the walk asked for a frame in %v, not at the flash's expiry", delay)
	}
}

// The same wiring drives the held-drag auto-scroll: with the pointer held at the
// viewport edge, each animation frame scrolls one line.
func TestNextAnimationStepsAHeldSelectionDrag(t *testing.T) {
	lines := make([]string, 0, 14)
	for index := range 14 {
		lines = append(lines, "line"+itoa(index))
	}
	screen := NewAltScreen(&recordingTerminal{width: 24, height: 6}, false, t.TempDir(), AltScreenOptions{})
	defer screen.Stop(TuiStopOptions{})
	screen.DisableAutoRender()
	screen.AddChild(&scriptedComponent{lines: lines})
	screen.Start()
	screen.RenderNow(false)
	before := screen.ViewportTop()

	// Press in the viewport, then drag to its top edge and hold.
	screen.HandleTerminalInput("\x1b[<0;3;4M")
	screen.HandleTerminalInput("\x1b[<32;3;1M")
	if screen.selectionAutoScrollDirection != -1 {
		t.Fatalf("drag at the edge set direction %d, want -1", screen.selectionAutoScrollDirection)
	}

	// The walk asks for the next frame, and advancing the clock past the cadence
	// steps the scroll without any further input.
	now := time.Now()
	screen.clock = func() time.Time { return now }
	if want, _ := screen.NextAnimation(); !want {
		t.Fatal("a held drag asked for no animation frame")
	}
	now = now.Add(2 * selectionAutoScrollIntervalMS * time.Millisecond)
	screen.NextAnimation()
	if got := screen.ViewportTop(); got >= before {
		t.Fatalf("viewport top = %d after a held drag, want below %d", got, before)
	}
}
