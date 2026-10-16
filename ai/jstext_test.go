package ai

import "testing"

func TestSanitizeSurrogatesReturnsGoStringWithoutAllocating(t *testing.T) {
	text := "plain text 😀"
	allocations := testing.AllocsPerRun(10, func() {
		if got := SanitizeSurrogates(text); got != text {
			t.Fatalf("sanitized = %q, want unchanged text", got)
		}
	})
	if allocations != 0 {
		t.Fatalf("sanitizing a Go string allocated %.0f objects, want 0", allocations)
	}
}

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

// JSLower must match String.prototype.toLowerCase, including the Greek
// final-sigma context rule and the U+0130 expansion.
func TestJSLowerMatchesJS(t *testing.T) {
	cases := map[string]string{
		"ΟΣ":  "ος", // final sigma
		"ΣΣ":  "σς",
		"İ":   "i\u0307",
		"ẞ":   "ß",
		"ABC": "abc",
	}
	for input, want := range cases {
		if got := JSLower(input); got != want {
			t.Errorf("JSLower(%q) = %q; want %q", input, got, want)
		}
	}
}

// JSNumberToFixed must match Number.prototype.toFixed: round half away from
// zero on the exact double value (not Go's %.nf round-half-to-even).
func TestJSNumberToFixedMatchesJS(t *testing.T) {
	cases := []struct {
		value  float64
		digits int
		want   string
	}{
		{1.25, 1, "1.3"},
		{1.15, 1, "1.1"}, // 1.15 is 1.14999... as a double
		{-1.25, 1, "-1.3"},
		{-0.001, 1, "-0.0"},
		{0, 1, "0.0"},
		{2.675, 2, "2.67"}, // 2.675 is 2.67499... as a double
		{1.005, 2, "1.00"},
		{3450, 0, "3450"},
	}
	for _, tc := range cases {
		if got := JSNumberToFixed(tc.value, tc.digits); got != tc.want {
			t.Errorf("JSNumberToFixed(%v, %d) = %q; want %q", tc.value, tc.digits, got, tc.want)
		}
	}
}
