package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
)

// A read result whose details name a model with no image input shows the warning
// on the card, collapsed or expanded: that model saw a placeholder, not the image
// (read.ts's renderer, v1.6.0, #12139).
func TestReadCardWarnsWhenTheModelCannotTakeImages(t *testing.T) {
	theme := newRendererTestTheme(t)
	details := []byte(`{"imageUnsupportedByModel":"text-only"}`)
	result := &SortToolResultContent{
		Content: []ToolResultContent{{Type: "text", Text: "Read image file [image/png]"}},
		Details: details,
	}
	for _, expanded := range []bool{false, true} {
		rendered := renderReadResultCard(t, result, expanded, theme)
		if !strings.Contains(rendered, "text-only does not support images; it saw a placeholder instead") {
			t.Errorf("expanded=%v card = %q, want the placeholder warning", expanded, rendered)
		}
	}
	// A result without the detail keeps the card quiet.
	plain := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: "Read image file [image/png]"}}}
	if rendered := renderReadResultCard(t, plain, true, theme); strings.Contains(rendered, "does not support images") {
		t.Fatalf("card = %q, want no warning", rendered)
	}
}

// renderReadResultCard renders the read renderer's result for a test.
func renderReadResultCard(t *testing.T, result *SortToolResultContent, expanded bool, theme *Theme) string {
	t.Helper()
	component := readRenderers.RenderResult(result, ToolRenderResultOptions{Expanded: expanded}, theme,
		&ToolRenderContext{Cwd: "/tmp/proj", ShowImages: false})
	return coding.StripAnsi(strings.Join(component.Render(80), "\n"))
}
