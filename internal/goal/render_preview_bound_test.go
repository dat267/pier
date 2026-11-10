package goal

import (
	"strings"
	"testing"
)

type previewTestTheme struct{}

func (previewTestTheme) Fg(_ string, text string) string { return text }
func (previewTestTheme) Bg(_ string, text string) string { return text }
func (previewTestTheme) Bold(text string) string         { return text }

// A collapsed goal card must stay small whatever the objective's line shape:
// capping the newline count alone leaves one long line unbounded, and
// tui.Text.Render then emits one output line per width band. 1 MB on a single
// line rendered 12,502 lines before this bound.
func TestGoalPreviewBoundsALongLine(t *testing.T) {
	cases := map[string]string{
		"single line": strings.Repeat("y", 1000000),
		"long lines":  strings.Repeat(strings.Repeat("z", 200000)+"\n", 20),
		"short lines": strings.Repeat("line\n", 100000),
	}
	for name, objective := range cases {
		lines := RenderSetGoalRenderCall(objective, previewTestTheme{}, false).Render(80)
		if len(lines) > 40 {
			t.Fatalf("%s: collapsed card rendered %d lines, want <= 40", name, len(lines))
		}
	}
}

// Expanding still shows the whole objective, and the collapsed form keeps the
// character budget it advertises through the hidden hint.
func TestGoalPreviewKeepsEverythingWhenExpanded(t *testing.T) {
	long := strings.Repeat("y", 5000)
	shown, hidden := goalPreview(long, true)
	if shown != long || hidden != 0 {
		t.Fatalf("expanded preview = %d chars, hidden %d, want the whole %d-char objective", len(shown), hidden, len(long))
	}
	collapsed, hidden := goalPreview(long, false)
	if hidden == 0 {
		t.Fatalf("collapsed preview hid nothing of a %d-char single line", len(long))
	}
	if len(collapsed) >= len(long) {
		t.Fatalf("collapsed preview returned %d of %d chars", len(collapsed), len(long))
	}
}
