package tui

import (
	"testing"
)

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

// tui trims and splits words on JavaScript's `\s` set, which includes U+202F,
// U+205F, U+3000 and the BOM but not U+0085 (unlike unicode.IsSpace).
func TestJSStringHelpers(t *testing.T) {
	for _, r := range []rune{0x202F, 0x205F, 0x3000, 0xFEFF} {
		if !isWhitespaceRune(r) {
			t.Errorf("isWhitespaceRune(U+%04X) = false; want true", r)
		}
	}
	if isWhitespaceRune(0x0085) {
		t.Error("isWhitespaceRune(U+0085) = true; want false")
	}
}
