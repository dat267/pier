package tui

import (
	"strings"
	"testing"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
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

// The JS string helpers must match String.prototype.trim/toLowerCase.
func TestJSStringHelpers(t *testing.T) {
	for _, r := range []rune{0x202F, 0x205F, 0x3000, 0xFEFF} {
		if got := jsTrim(string(r) + "x" + string(r)); got != "x" {
			t.Errorf("jsTrim(U+%04X) = %q; want x", r, got)
		}
	}
	if got := jsTrim("\u0085"); got != "\u0085" {
		t.Errorf("jsTrim(U+0085) = %q; want unchanged", got)
	}
	if got := jsLower("ΟΣ"); got != "ος" {
		t.Errorf("jsLower(ΟΣ) = %q; want ος", got)
	}
	if got := jsLower("İ"); got != "i\u0307" {
		t.Errorf("jsLower(İ) = %q; want i\\u0307", got)
	}
	// D518: the ASCII fast path agrees with the full mapping on every byte.
	var ascii strings.Builder
	for c := range 128 {
		ascii.WriteByte(byte(c))
	}
	if got, want := jsLower(ascii.String()), cases.Lower(language.Und).String(ascii.String()); got != want {
		t.Errorf("jsLower(ASCII) = %q; want %q", got, want)
	}
}
