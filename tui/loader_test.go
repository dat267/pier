package tui

import (
	"strings"
	"testing"
	"time"
)

// D402: an elapsed label follows the message and keeps the loader animating
// once a second, even when the indicator has no spinner frames.
func TestLoaderElapsedLabelTicksEachSecond(t *testing.T) {
	loader := NewLoader(nil, nil, nil, "Working", &LoaderIndicatorOptions{Frames: []string{}, HasFrames: true})
	if need, _ := loader.AnimationFrame(time.Now()); need {
		t.Fatal("a frameless loader without a label should not animate")
	}
	since := time.Now().Add(-2500 * time.Millisecond)
	loader.SetElapsed(since, func(d time.Duration) string { return "(" + d.Truncate(time.Second).String() + ")" })
	if got := StripTerminalSequences(strings.Join(loader.Render(40), "\n")); !strings.Contains(got, "Working (2s)") {
		t.Fatalf("render = %q", got)
	}
	need, next := loader.AnimationFrame(since.Add(2500 * time.Millisecond))
	if !need || next != 500*time.Millisecond {
		t.Fatalf("AnimationFrame = %v, %v; want the next whole second", need, next)
	}
	empty := NewLoader(nil, nil, nil, "", &LoaderIndicatorOptions{Frames: []string{}, HasFrames: true})
	empty.SetElapsed(time.Now(), func(time.Duration) string { return "(0s)" })
	if got := StripTerminalSequences(strings.Join(empty.Render(40), "\n")); strings.Contains(got, "  (0s)") || !strings.Contains(got, " (0s)") {
		t.Fatalf("empty-message render = %q", got)
	}
}

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
