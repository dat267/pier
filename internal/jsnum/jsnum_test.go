package jsnum

import (
	"encoding/json"
	"math"
	"slices"
	"testing"
	"time"
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

// ArrayIndex is ECMAScript's array-index test (CanonicalNumericIndexString
// below 2^32-1), which orders Object.keys and JSON.stringify output.
func TestArrayIndexAcceptsOnlyCanonicalIndices(t *testing.T) {
	for key, want := range map[string]int64{
		"0": 0, "7": 7, "10": 10, "4294967294": 4294967294,
		"": -1, "01": -1, "-0": -1, "-1": -1, "1.5": -1, "1e3": -1, " 1": -1,
		"4294967295": -1, "4294967296": -1, "99999999999": -1, "a": -1,
	} {
		index, ok := ArrayIndex(key)
		if ok != (want >= 0) || (ok && int64(index) != want) {
			t.Errorf("ArrayIndex(%q) = %d, %v; want %d", key, index, ok, want)
		}
	}
	if allocs := testing.AllocsPerRun(100, func() { ArrayIndex("ordinary") }); allocs != 0 {
		t.Fatalf("ArrayIndex allocates %v times for an ordinary key", allocs)
	}
}

func TestObjectKeyOrderPutsIndicesFirstAscending(t *testing.T) {
	got := ObjectKeyOrder([]string{"b", "10", "a", "2", "01", "4294967295", "0"})
	want := []string{"0", "2", "10", "b", "a", "01", "4294967295"}
	if !slices.Equal(got, want) {
		t.Fatalf("ObjectKeyOrder = %q; want %q", got, want)
	}
}

func TestToIntSaturatesOutOfRange(t *testing.T) {
	if got := ToInt(1e300); got != math.MaxInt {
		t.Errorf("ToInt(1e300) = %d", got)
	}
	if got := ToInt(-1e300); got != math.MinInt {
		t.Errorf("ToInt(-1e300) = %d", got)
	}
	if got := ToInt(-2.9); got != -2 {
		t.Errorf("ToInt(-2.9) = %d; want -2", got)
	}
	if got := ToInt64(1e19); got != math.MaxInt64 {
		t.Errorf("ToInt64(1e19) = %d", got)
	}
	if got := ToInt64(-1e19); got != math.MinInt64 {
		t.Errorf("ToInt64(-1e19) = %d", got)
	}
}

// Node and Bun run setTimeout(fn, ms) after 1 ms when ms is below 1 or above
// 2^31-1, and after ms otherwise.
func TestTimeoutDurationClampsLikeSetTimeout(t *testing.T) {
	for ms, want := range map[int64]time.Duration{
		-5: time.Millisecond, 0: time.Millisecond, 1: time.Millisecond, 250: 250 * time.Millisecond,
		MaxTimeoutMS: time.Duration(MaxTimeoutMS) * time.Millisecond, MaxTimeoutMS + 1: time.Millisecond,
	} {
		if got := TimeoutDuration(ms); got != want {
			t.Errorf("TimeoutDuration(%d) = %v, want %v", ms, got, want)
		}
	}
}
