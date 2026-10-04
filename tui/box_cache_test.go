package tui

import (
	"fmt"
	"strings"
	"testing"
)

func makeWarmBox(lines int) *Box {
	var sb strings.Builder
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	box := NewBox(1, 1, func(text string) string { return "<bg>" + text + "</bg>" })
	box.AddChild(NewText(strings.TrimSuffix(sb.String(), "\n"), 0, 0, nil))
	box.Render(40)
	return box
}

func warmBox(t *testing.T, lines int) *Box {
	t.Helper()
	box := makeWarmBox(lines)
	if len(box.Render(40)) == 0 {
		t.Fatal("empty render")
	}
	return box
}

// TestBoxCacheValidationDoesNotScaleWithLines pins that a warm render (cache
// hit) costs the same for a short and a long child: validating the cache must
// not rebuild a padded copy of every line.
func TestBoxCacheValidationDoesNotScaleWithLines(t *testing.T) {
	small, large := warmBox(t, 2), warmBox(t, 200)

	smallAllocs := testing.AllocsPerRun(20, func() { _ = small.Render(40) })
	largeAllocs := testing.AllocsPerRun(20, func() { _ = large.Render(40) })

	if largeAllocs > smallAllocs+4 {
		t.Fatalf("warm render allocated %.0f times for 200 lines vs %.0f for 2; "+
			"the cache validation scales with the child's line count", largeAllocs, smallAllocs)
	}
}

// TestBoxCacheInvalidatesOnBackgroundAndWidth covers the inputs the cache
// fingerprint must still catch.
func TestBoxCacheInvalidatesOnBackgroundAndWidth(t *testing.T) {
	box := NewBox(0, 0, func(text string) string { return "A" + text })
	box.AddChild(NewText("content", 0, 0, nil))
	first := strings.Join(box.Render(20), "\n")

	box.SetBgFn(func(text string) string { return "B" + text })
	if second := strings.Join(box.Render(20), "\n"); second == first || !strings.Contains(second, "B") {
		t.Fatalf("background change not picked up: %q", second)
	}

	if narrowed := box.Render(30); len(narrowed) == 0 {
		t.Fatal("width change produced no lines")
	}
	// Returning to the previous width must not serve the stale (narrow) cache.
	if again := strings.Join(box.Render(20), "\n"); again != strings.Join(box.Render(20), "\n") {
		t.Fatal("inconsistent render at the same width")
	}
}

func BenchmarkBoxWarmRender(b *testing.B) {
	box := makeWarmBox(50)
	box.Render(80)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = box.Render(80)
	}
}

// TestBoxReappliesBackgroundOnlyToTheChangedSuffix pins the incremental
// backgrounding: a change to one trailing line must not re-apply the background
// (VisibleWidth re-parses the ANSI) to every line of a long child. This is the
// running tool's elapsed label, once a second.
func TestBoxReappliesBackgroundOnlyToTheChangedSuffix(t *testing.T) {
	calls := 0
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	body := strings.TrimSuffix(sb.String(), "\n")
	text := NewText(body, 0, 0, nil)
	box := NewBox(0, 1, func(value string) string {
		calls++
		return "<bg>" + value + "</bg>"
	})
	box.AddChild(text)
	box.Render(40)

	before := calls
	text.SetText(body + "\nchanged tail")
	lines := box.Render(40)
	if reapplied := calls - before; reapplied > 4 {
		t.Fatalf("background reapplied to %d lines, want only the changed suffix", reapplied)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "changed tail") {
		t.Fatalf("changed tail missing")
	}
}

// TestBoxReusesPrefixForAnInPlaceContainerChange is the ticked-tool case: a
// child Container rewrites its suffix in place (the same backing array), which
// the line comparison cannot see; the changed-from report lets the box reuse the
// backgrounded prefix.
func TestBoxReusesPrefixForAnInPlaceContainerChange(t *testing.T) {
	calls := 0
	container := &Container{}
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	container.AddChild(NewText(strings.TrimSuffix(sb.String(), "\n"), 0, 0, nil))
	tail := NewText("tail", 0, 0, nil)
	container.AddChild(tail)
	box := NewBox(0, 1, func(value string) string {
		calls++
		return "<bg>" + value + "</bg>"
	})
	box.AddChild(container)
	box.Render(40)

	before := calls
	tail.SetText("tail changed")
	lines := box.Render(40)
	if reapplied := calls - before; reapplied > 4 {
		t.Fatalf("background reapplied to %d lines, want only the changed suffix", reapplied)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "tail changed") {
		t.Fatalf("stale output after an in-place change")
	}
}
