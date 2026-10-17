package tui

import (
	"testing"
	"time"
)

// invalidateCountingChild counts Invalidate calls that reach it.
type invalidateCountingChild struct {
	*Container
	invalidates int
}

func (c *invalidateCountingChild) Invalidate() {
	c.invalidates++
	c.Container.Invalidate()
}

// TestScrollViewAnimationTickDoesNotInvalidateContent pins the narrow tick: the
// transient scrollbar lives in the view's state and is composited by the layout
// painter, so an animation frame must request a repaint without invalidating the
// scrolled document. Invalidating the view cascaded through every mounted tool
// and re-ran its display build once per animation frame.
func TestScrollViewAnimationTickDoesNotInvalidateContent(t *testing.T) {
	child := &invalidateCountingChild{Container: &Container{}}
	child.Container.AddChild(NewText("hello", 0, 0, nil))
	view := NewScrollView(child, ScrollViewOptions{Primary: true})
	if _, ok := any(view).(AnimationTicker); !ok {
		t.Fatal("ScrollView must implement AnimationTicker so the animation walk does not Invalidate the scrolled document")
	}
	child.invalidates = 0
	view.AnimationTick()
	if child.invalidates != 0 {
		t.Fatalf("AnimationTick invalidated the scrolled child %d times, want 0", child.invalidates)
	}
	// The generic Animator tick still invalidates.
	view.Invalidate()
	if child.invalidates == 0 {
		t.Fatal("Invalidate no longer reaches the scrolled child")
	}
	// A visible transient scrollbar still reports an animation and requests a
	// repaint so the layout repaints the scrollbar.
	view.scrollbarHideDeadline = time.Now().Add(time.Second)
	wanted := 0
	view.requestRenderCallback = func() { wanted++ }
	if want, _ := view.AnimationFrame(time.Now()); !want {
		t.Fatal("a visible transient scrollbar must ask for an animation frame")
	}
	view.AnimationTick()
	if wanted != 1 {
		t.Fatalf("AnimationTick requested %d repaints, want 1", wanted)
	}
}
