package tui

import "testing"

// packages/tui/src/components/text.ts suppresses text whose String.trim() is empty.
func TestTextRenderUsesJavaScriptTrim(t *testing.T) {
	if got := NewText("\uFEFF", 0, 0, nil).Render(80); len(got) != 0 {
		t.Fatalf("Render(BOM) = %#v; want no lines", got)
	}
}
