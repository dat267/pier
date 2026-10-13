package interactive

import (
	"strings"
	"testing"
)

// v1.2.0 #10809/#10810: a compaction indicator embedded in a custom editor
// uses the editor's border color (or the thinking-level color), like the
// working indicator, instead of the default muted colors.
func TestEmbeddedCompactionStatusUsesEditorBorderColor(t *testing.T) {
	state, editor := newUIStateTestState(t, true)
	editor.BorderColor = func(text string) string { return "<" + text + ">" }
	indicator := NewCompactionStatusIndicator(state.UI, "manual", state.EmbeddedStatusColorFn("off"))
	state.ShowStatusIndicator(indicator)
	if editor.GetWorkingStatusIndicator() != indicator {
		t.Fatal("the compaction indicator was not embedded")
	}
	line := indicator.RenderInBorder(40)
	if !strings.Contains(line, "<") {
		t.Fatalf("the embedded compaction indicator ignored the border color: %q", line)
	}
}

func TestCompactionStatusWithoutEmbeddingKeepsDefaultColors(t *testing.T) {
	state, _ := newUIStateTestState(t, false)
	if colorFn := state.EmbeddedStatusColorFn("off"); colorFn != nil {
		t.Fatal("a non-embedding editor produced a color function")
	}
}
