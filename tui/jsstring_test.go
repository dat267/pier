package tui

import "testing"

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
}
