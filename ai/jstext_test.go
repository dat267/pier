package ai

import "testing"

// JSTrim must match String.prototype.trim(): the ECMAScript WhiteSpace and
// LineTerminator sets. U+0085 is not in that set, while the Zs separators
// U+202F, U+205F and U+3000 are.
func TestJSTrimMatchesJSWhitespace(t *testing.T) {
	trimmed := []rune{
		'\t', '\n', '\v', '\f', '\r', ' ',
		0x00A0, 0x1680, 0x2000, 0x200A, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF,
	}
	for _, r := range trimmed {
		input := string(r) + "x" + string(r)
		if got := JSTrim(input); got != "x" {
			t.Errorf("JSTrim(U+%04X) = %q; want %q", r, got, "x")
		}
	}
	kept := []rune{0x0085, 0x180E, 0x200B, 0x200C}
	for _, r := range kept {
		input := string(r) + "x" + string(r)
		if got := JSTrim(input); got != input {
			t.Errorf("JSTrim(U+%04X) = %q; want unchanged", r, got)
		}
	}
}
