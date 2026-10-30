package tui

import (
	"testing"
	"time"
)

// pi v1.4.0 scroll-view.ts schedules the hide with setTimeout, which clamps
// delays above 2^31-1 to 1ms.
func TestScrollViewHideDelayUsesJavaScriptTimerClamp(t *testing.T) {
	view := NewScrollView(probe("A", "B", "C", "D", "E", "F"), ScrollViewOptions{
		Scrollbar: ScrollbarAuto, HasScrollbarHideDelay: true, ScrollbarHideDelayMS: 1 << 31,
	})
	RenderLayoutFrame(view, 6, 3, func() {})
	view.SetScrollbarActive(true)
	view.SetScrollbarActive(false)

	active, delay := view.AnimationFrame(time.Now())
	if !active || delay <= 0 || delay > time.Millisecond {
		t.Fatalf("animation active=%v delay=%s; want hide deadline within 1ms", active, delay)
	}
}
