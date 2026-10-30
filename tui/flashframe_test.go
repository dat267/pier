package tui

import (
	"testing"
	"time"
)

// newFlashTestScreen builds a started alt screen with one child, the shape the
// golden replay tests use.
func newFlashTestScreen(t *testing.T) *AltScreen {
	t.Helper()
	screen := NewAltScreen(&recordingTerminal{width: 24, height: 6}, false, t.TempDir(), AltScreenOptions{})
	screen.DisableAutoRender()
	screen.AddChild(&scriptedComponent{lines: []string{"content"}})
	screen.Start()
	return screen
}

// TestFlashingScreenAsksForAFrameAtTheFlashExpiry pins that the animation walk
// knows a flash is pending.
//
// Upstream gives each flash its own setTimeout whose callback removes the entry
// and calls requestRender(), so a flash lives exactly its duration. The port has
// no internal timer: the hide is a deadline the consumer's animation walk
// has to wake on. But the flash container is a field composited over the screen
// rather than a child in the tree, so the walk cannot reach it, and AltScreen's
// own AnimationFrame considered only the selection drag. Nothing asked for a
// frame at the expiry, so a flash outlived its duration until some unrelated
// repaint — an idle session kept "Copied!" on screen indefinitely.
func TestFlashingScreenAsksForAFrameAtTheFlashExpiry(t *testing.T) {
	screen := newFlashTestScreen(t)
	if want, _ := screen.AnimationFrame(time.Now()); want {
		t.Fatal("an idle alt screen asked for an animation frame")
	}

	screen.Flash("Copied!")

	want, delay := screen.AnimationFrame(time.Now())
	if !want {
		t.Fatal("a flash was pending and the screen asked for no frame: nothing wakes the walk at its expiry")
	}
	if delay <= 0 || delay > 1100*time.Millisecond {
		t.Fatalf("the screen asked for a frame in %v, not at the flash's expiry", delay)
	}
}

// TestExpiredFlashIsGoneAndRepainted pins the other half: when the deadline
// passes, the entry is dropped and a frame is asked for, the way upstream's timer
// requests a render as it removes the entry.
// pi v1.4.0 alt-screen-flash.ts defaults only an omitted duration; explicit
// zero reaches setTimeout and uses JavaScript's 1ms timer minimum.
func TestExplicitZeroFlashUsesTimerMinimum(t *testing.T) {
	flashes := NewAltScreenFlashContainer(nil)
	flashes.Flash("instant", 0)
	want, delay := flashes.AnimationFrame(time.Now())
	if !want || delay <= 0 || delay > 2*time.Millisecond {
		t.Fatalf("zero-duration flash frame = (%t, %v), want a timer-minimum deadline", want, delay)
	}
}

func TestOversizedFlashDurationUsesJavaScriptTimerClamp(t *testing.T) {
	flashes := NewAltScreenFlashContainer(nil)
	flashes.Flash("short-lived", 1<<31)
	want, delay := flashes.AnimationFrame(time.Now())
	if !want || delay <= 0 || delay > 2*time.Millisecond {
		t.Fatalf("oversized flash frame = (%t, %v), want a 1ms JavaScript timer", want, delay)
	}
}

func TestExpiredFlashIsGoneAndRepainted(t *testing.T) {
	flashes := NewAltScreenFlashContainer(nil)
	flashes.Flash("Copied!")

	later := time.Now().Add(2 * time.Second)
	want, delay := flashes.AnimationFrame(later)
	if !want || delay <= 0 {
		t.Fatal("an expired flash was dropped without asking for a frame to paint its removal")
	}
	if lines := flashes.Render(20); len(lines) != 0 {
		t.Fatalf("the expired flash was still rendered: %q", lines)
	}
	if want, _ := flashes.AnimationFrame(later); want {
		t.Fatal("the container asked for a frame with nothing left to show")
	}
}
