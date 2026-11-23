package jsnum

import (
	"encoding/json"
	"math"
	"testing"
	"unicode"
)

// The cases mirror the JavaScript Number() results the coercion must match;
// callers (mcp/oauth's expires_in, ai's federation expires_in) rely on them.
func TestNumberMatchesJavaScriptCoercion(t *testing.T) {
	cases := []struct {
		label string
		value any
		want  float64
		ok    bool
	}{
		{"number", float64(3600), 3600, true},
		{"fraction", 1.5, 1.5, true},
		{"zero", float64(0), 0, true},
		{"negative", float64(-5), -5, true},
		{"json number", json.Number("42"), 42, true},
		{"bad json number", json.Number("x"), 0, false},
		{"numeric string", "3600", 3600, true},
		{"padded string", "  3600\t", 3600, true},
		{"empty string", "", 0, true},
		{"hex string", "0x10", 16, true},
		{"binary string", "0b101", 5, true},
		{"octal string", "0o17", 15, true},
		{"separator string", "1_0", 0, false},
		{"non-numeric string", "soon", 0, false},
		{"null", nil, 0, true},
		{"true", true, 1, true},
		{"false", false, 0, true},
		// A radix prefix with no digits is NaN upstream ("0x" is not 0), which is
		// the boundary the `len(text) > 2` guard before the prefix switch exists for.
		{"hex prefix only", "0x", 0, false},
		{"binary prefix only", "0b", 0, false},
		{"octal prefix only", "0o", 0, false},
		{"hex zero", "0x0", 0, true},
		{"empty array", []any{}, 0, true},
		{"single element array", []any{float64(7)}, 7, true},
		{"nested array", []any{[]any{"8"}}, 8, true},
		{"two element array", []any{1.0, 2.0}, 0, false},
		{"object", map[string]any{}, 0, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.label, func(t *testing.T) {
			got, ok := Number(testCase.value)
			if ok != testCase.ok {
				t.Fatalf("ok = %v, want %v", ok, testCase.ok)
			}
			if ok && got != testCase.want {
				t.Fatalf("value = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestNumberRejectsInfinitiesLikeIsFinite(t *testing.T) {
	for _, value := range []any{"Infinity", "-Infinity", "NaN", math.Inf(1), math.Inf(-1), math.NaN()} {
		if got, ok := Number(value); ok {
			t.Fatalf("Number(%v) = %v, want not finite", value, got)
		}
	}
}

func TestWhitespaceExcludesNextLineAndIncludesBOM(t *testing.T) {
	if !Whitespace('\uFEFF') || !Whitespace(' ') || !Whitespace('\u3000') {
		t.Fatal("JavaScript whitespace must include the BOM and the ideographic space")
	}
	if Whitespace('\u0085') {
		t.Fatal("U+0085 is Go whitespace but not JavaScript whitespace")
	}
	// The hand-written switch is the Unicode space property minus U+0085, plus
	// the BOM, for every code point.
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if want := r == '\uFEFF' || (r != '\u0085' && unicode.IsSpace(r)); Whitespace(r) != want {
			t.Fatalf("Whitespace(U+%04X) = %v; want %v", r, !want, want)
		}
	}
}
