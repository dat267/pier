package tui

import (
	"testing"
	"time"
)

// pi v1.4.0 tui/components/loader.ts uses setInterval for frame updates, which
// clamps intervals above 2^31-1 to 1ms.
func TestLoaderIntervalUsesJavaScriptTimerClamp(t *testing.T) {
	loader := NewLoader(nil, func(text string) string { return text }, func(text string) string { return text }, "Loading",
		&LoaderIndicatorOptions{Frames: []string{"a", "b"}, HasFrames: true, IntervalMS: 1 << 31})
	defer loader.Stop()

	active, delay := loader.AnimationFrame(loader.startedAt)
	if !active || delay != time.Millisecond {
		t.Fatalf("animation active=%v delay=%s; want active at 1ms", active, delay)
	}
	if got := loader.RenderedIndicatorAt(loader.startedAt.Add(time.Millisecond)); got != "b" {
		t.Fatalf("frame after 1ms = %q, want second frame", got)
	}
}
