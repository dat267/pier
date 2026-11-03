package tui

import (
	"strings"
	"testing"
)

// Streaming renders must reuse the cached block-key and line backing arrays;
// reallocating them per delta made long messages quadratic (M143 streaming
// follow-up).
func TestMarkdownStreamRenderReusesCacheSlices(t *testing.T) {
	var text strings.Builder
	for range 10 {
		text.WriteString("Paragraph with **bold** and `code`.\n\n")
	}
	m := NewMarkdown(text.String(), 1, 0, mdTestTheme(), nil, MarkdownOptions{})
	_ = m.Render(80)
	// One append crosses the initial exact-size capacity and doubles it.
	text.WriteString("Growth paragraph.\n\n")
	m.SetText(text.String())
	_ = m.Render(80)
	if len(m.cacheTokenKeys) == 0 || len(m.cacheTokenLines) == 0 {
		t.Fatal("cache not populated")
	}
	keyBacking := &m.cacheTokenKeys[0]
	lineBacking := &m.cacheTokenLines[0]
	for range 5 {
		text.WriteString("Another paragraph.\n\n")
		m.SetText(text.String())
		_ = m.Render(80)
		if &m.cacheTokenKeys[0] != keyBacking {
			t.Fatal("block-key slice backing was reallocated within capacity")
		}
		if &m.cacheTokenLines[0] != lineBacking {
			t.Fatal("block-line slice backing was reallocated within capacity")
		}
	}
}
