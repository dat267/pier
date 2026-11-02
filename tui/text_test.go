package tui

import "testing"

// pi v1.5.0 text.ts setText returns early for the same text, so a renderer that
// republishes unchanged content does not discard its wrapped lines.
func TestTextSetSameTextKeepsCache(t *testing.T) {
	text := NewText("hello world", 0, 0, nil)
	_ = text.Render(20)
	if !text.hasCachedLines {
		t.Fatal("setup: render did not cache lines")
	}
	text.SetText("hello world")
	if !text.hasCachedLines {
		t.Fatal("re-setting the same text dropped the wrap cache")
	}
	text.SetText("changed")
	if text.hasCachedLines {
		t.Fatal("changing the text did not drop the wrap cache")
	}
}
