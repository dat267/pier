package interactive

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/tui"
)

// TestCustomEditorEmbeddedStatusAnimates pins the loop-mode animation seam:
// the working/compaction indicator is embedded in the editor's border and is
// not a container child, so the renderer's animation walk can only find it if
// the editor itself reports as an Animator. Regression for the frozen
// "⠋ Compacting..." frame (no repaints happen during compaction, so
// the time-derived frame never advanced).
func TestCustomEditorEmbeddedStatusAnimates(t *testing.T) {
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	SetRegisteredThemes(nil)
	InitTheme("dark", false)
	editor := NewCustomEditor(nil, tui.EditorTheme{}, nil, CustomEditorOptions{EmbedWorkingStatus: true})
	indicator := NewCompactionStatusIndicator(nil, "manual", nil)
	editor.SetWorkingStatusIndicator(indicator)

	animator, ok := any(editor).(tui.Animator)
	if !ok {
		t.Fatal("CustomEditor with an embedded status indicator must implement tui.Animator")
	}
	want, _ := animator.AnimationFrame(time.Now())
	if !want {
		t.Fatal("embedded status indicator must want animation frames")
	}

	editor.SetWorkingStatusIndicator(nil)
	want, _ = animator.AnimationFrame(time.Now())
	if want {
		t.Fatal("editor without an embedded indicator must not want animation frames")
	}
}

// TestCompactionIndicatorDrivesAnimation drives the app seam: after the
// compaction status indicator is shown (embedded in the editor border), the
// renderer's NextAnimation query must report a pending frame so the run loop
// keeps painting the spinner.
func TestCompactionIndicatorDrivesAnimation(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	app.Init(context.Background())

	app.uiState.ShowStatusIndicator(NewCompactionStatusIndicator(app.ui, "manual", nil))
	want, _ := app.ui.NextAnimation()
	if !want {
		t.Fatal("renderer animation walk must see the editor-embedded compaction indicator")
	}
	app.uiState.ClearStatusIndicator("", false)
	want, _ = app.ui.NextAnimation()
	if want {
		t.Fatal("no animation wanted after the indicator is cleared")
	}
}

// TestCompactionIndicatorShowsOneTickingLabel pins D405: pier prints one
// "Compacting" label for every reason and appends the working indicator's
// elapsed tick, where upstream v1.5.0 (packages/coding-agent/src/modes/
// interactive/components/status-indicator.ts CompactionStatusIndicator) prints
// the reason as text, no clock and a cancel hint: "Compacting context...
// (escape to cancel)", "Auto-compacting... (escape to cancel)" or "Context
// overflow detected, Auto-compacting... (escape to cancel)". Escape still
// aborts compaction (handleCompactionStart swaps Editor.OnEscape); only the
// hint is gone.
func TestCompactionIndicatorShowsOneTickingLabel(t *testing.T) {
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	SetRegisteredThemes(nil)
	InitTheme("dark", false)
	for _, reason := range []string{"manual", "threshold", "overflow"} {
		indicator := NewCompactionStatusIndicator(nil, reason, nil)
		text := strings.Join(indicator.Render(120), "\n")
		indicator.Dispose()
		want := "Compacting (0s)"
		if !strings.Contains(text, want) {
			t.Fatalf("%s: render = %q, want %q", reason, text, want)
		}
		for _, gone := range []string{"context", "Auto-compacting", "overflow detected", "cancel"} {
			if strings.Contains(text, gone) {
				t.Fatalf("%s: render still carries %q: %q", reason, gone, text)
			}
		}
	}
	// The suffix is the clock, not a constant: the same indicator counts from
	// the reading it was built with, and a longer run renders the minutes form.
	short := newCompactionStatusIndicator(nil, nil, time.Now().Add(-12*time.Second))
	defer short.Dispose()
	if text := strings.Join(short.Render(120), "\n"); !strings.Contains(text, "Compacting (12s)") {
		t.Fatalf("render = %q, want the 12s tick", text)
	}
	long := newCompactionStatusIndicator(nil, nil, time.Now().Add(-65*time.Second))
	defer long.Dispose()
	if text := strings.Join(long.Render(120), "\n"); !strings.Contains(text, "Compacting (1m 5s)") {
		t.Fatalf("render = %q, want the 1m 5s tick", text)
	}
	// The line keeps the loader's shape at a narrow width: an empty first line
	// and a second one padded out to the width it was rendered at.
	lines := long.Render(60)
	if len(lines) != 2 || lines[0] != "" || tui.VisibleWidth(lines[1]) != 60 {
		t.Fatalf("Render(60) = %q, want an empty line then a 60-cell line", lines)
	}
	// A running indicator has to ask for a repaint inside one second, or the
	// tick would freeze between spinner frames (the seam D402 needed for the
	// working clock).
	if frame, next := long.AnimationFrame(time.Now()); !frame || next <= 0 || next > time.Second {
		t.Fatalf("AnimationFrame = %v, %v; want a frame within one second", frame, next)
	}
}
