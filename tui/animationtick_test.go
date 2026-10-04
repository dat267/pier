package tui

import (
	"testing"
	"time"
)

// gatedAnimator counts invalidations, standing in for a running tool whose
// Invalidate rebuilds its whole output.
type gatedAnimator struct {
	delay  time.Duration
	frames int
}

func (a *gatedAnimator) Render(int) []string { return nil }
func (a *gatedAnimator) Invalidate()         { a.frames++ }
func (a *gatedAnimator) AnimationFrame(now time.Time) (bool, time.Duration) {
	return true, a.delay
}

// TestAnimationWalkTicksAnAnimatorOncePerDelay pins the scroll-lag fix: the
// animation walk runs on every paint, but an animator is invalidated only once
// per its own delay. Ticking it on every walk invalidated (and rebuilt the
// whole subtree of) every running tool on every frame.
func TestAnimationWalkTicksAnAnimatorOncePerDelay(t *testing.T) {
	animator := &gatedAnimator{delay: time.Second}
	ticks := map[Component]time.Time{}
	base := time.Now()

	if needs, _ := nextAnimationForTicked([]Component{animator}, base, ticks); !needs {
		t.Fatal("the walk did not report the animator")
	}
	if animator.frames != 1 {
		t.Fatalf("invalidations = %d on the first walk, want 1", animator.frames)
	}
	// A walk inside the delay must not tick again.
	nextAnimationForTicked([]Component{animator}, base.Add(500*time.Millisecond), ticks)
	if animator.frames != 1 {
		t.Fatalf("invalidations = %d after 0.5s, want 1", animator.frames)
	}
	// Once the delay has elapsed, the next walk ticks again.
	nextAnimationForTicked([]Component{animator}, base.Add(1100*time.Millisecond), ticks)
	if animator.frames != 2 {
		t.Fatalf("invalidations = %d after 1.1s, want 2", animator.frames)
	}
	// A walk without tick state ticks every time (the pre-fix behavior).
	fresh := &gatedAnimator{delay: time.Second}
	nextAnimationForTicked([]Component{fresh}, base, nil)
	nextAnimationForTicked([]Component{fresh}, base.Add(time.Millisecond), nil)
	if fresh.frames != 2 {
		t.Fatalf("nil-tick invalidations = %d, want 2", fresh.frames)
	}
	// An unmounted animator drops its tick entry.
	nextAnimationForTicked(nil, base.Add(2*time.Second), ticks)
	if len(ticks) != 0 {
		t.Fatalf("unmounted animator left %d tick entries", len(ticks))
	}
}

// tickingAnimator implements both Animator and AnimationTicker.
type tickingAnimator struct {
	delay  time.Duration
	frames int
	ticks  int
}

func (a *tickingAnimator) Render(int) []string                                { return nil }
func (a *tickingAnimator) Invalidate()                                        { a.frames++ }
func (a *tickingAnimator) AnimationFrame(now time.Time) (bool, time.Duration) { return true, a.delay }
func (a *tickingAnimator) AnimationTick()                                     { a.ticks++ }

// TestAnimationWalkUsesTheNarrowTick pins the D190 hook: an animator that
// implements AnimationTicker is ticked, not invalidated, so its subtree's
// render caches survive the clock tick.
func TestAnimationWalkUsesTheNarrowTick(t *testing.T) {
	animator := &tickingAnimator{delay: time.Second}
	nextAnimationForTicked([]Component{animator}, time.Now(), map[Component]time.Time{})
	if animator.ticks != 1 {
		t.Fatalf("ticks = %d, want 1", animator.ticks)
	}
	if animator.frames != 0 {
		t.Fatalf("Invalidate called %d times; the narrow tick must not invalidate", animator.frames)
	}
}
