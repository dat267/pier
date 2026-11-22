package ai

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

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

// Reference: pi v1.4.0 core/tools/truncate.ts and utils/image-resize.ts use
// Number.prototype.toFixed. At magnitude 1e21 it delegates to Number.toString,
// regardless of the requested fraction digits.
func TestJSNumberToFixedUsesExponentForLargeMagnitudes(t *testing.T) {
	for _, tc := range []struct {
		value float64
		want  string
	}{
		{1e21, "1e+21"},
		{-1e21, "-1e+21"},
		{1.25e22, "1.25e+22"},
	} {
		if got := JSNumberToFixed(tc.value, 2); got != tc.want {
			t.Errorf("JSNumberToFixed(%v, 2) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

// JSSlice is documented as String.prototype.slice, which resolves negative
// indices from the end.
func TestJSSliceMatchesJSNegativeIndices(t *testing.T) {
	cases := []struct {
		text       string
		start, end int
		want       string
	}{
		{"abcdef", -2, 6, "ef"},
		{"abcdef", 1, -1, "bcde"},
		{"abcdef", -100, 100, "abcdef"},
		{"abcdef", 2, 1, ""},
		// UTF-16 units: a(1) + emoji(2) + b(1); slice(1, 3) is the emoji.
		{"a😀b", 1, 3, "😀"},
	}
	for _, tc := range cases {
		if got := JSSlice(tc.text, tc.start, tc.end); got != tc.want {
			t.Errorf("JSSlice(%q, %d, %d) = %q; want %q", tc.text, tc.start, tc.end, got, tc.want)
		}
	}
}

// Replays the Node-generated fixture for the JavaScript text and number
// helpers: Number.prototype.toFixed and String(number) across the fixed and
// exponential ranges, toLowerCase with its full mappings (final sigma, İ),
// String.prototype.slice over negative, astral and out-of-range indices, and
// String.prototype.trim over the ECMAScript whitespace class.
func TestJSTextMatchesJavaScript(t *testing.T) {
	data, err := os.ReadFile("testdata/js-text/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Fixed []struct {
			N   float64 `json:"n"`
			D   int     `json:"d"`
			Out string  `json:"out"`
		} `json:"fixed"`
		Strings []struct {
			N   float64 `json:"n"`
			Out string  `json:"out"`
		} `json:"strings"`
		Lowers []struct {
			S   string `json:"s"`
			Out string `json:"out"`
		} `json:"lowers"`
		Slices []struct {
			S   string `json:"s"`
			A   int    `json:"a"`
			B   int    `json:"b"`
			Out string `json:"out"`
		} `json:"slices"`
		Trims []struct {
			S   string `json:"s"`
			Out string `json:"out"`
		} `json:"trims"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	total := len(corpus.Fixed) + len(corpus.Strings) + len(corpus.Lowers) + len(corpus.Slices) + len(corpus.Trims)
	if total == 0 {
		t.Fatal("empty js-text golden")
	}
	for _, testCase := range corpus.Fixed {
		if got := JSNumberToFixed(testCase.N, testCase.D); got != testCase.Out {
			t.Errorf("toFixed(%v, %d) = %q, want %q", testCase.N, testCase.D, got, testCase.Out)
		}
	}
	for _, testCase := range corpus.Strings {
		if got := JSNumberToString(testCase.N); got != testCase.Out {
			t.Errorf("String(%v) = %q, want %q", testCase.N, got, testCase.Out)
		}
	}
	for _, testCase := range corpus.Lowers {
		if got := JSLower(testCase.S); got != testCase.Out {
			t.Errorf("toLowerCase(%q) = %q, want %q", testCase.S, got, testCase.Out)
		}
	}
	for _, testCase := range corpus.Slices {
		if got := JSSlice(testCase.S, testCase.A, testCase.B); got != testCase.Out {
			t.Errorf("slice(%q, %d, %d) = %q, want %q", testCase.S, testCase.A, testCase.B, got, testCase.Out)
		}
	}
	for _, testCase := range corpus.Trims {
		if got := JSTrim(testCase.S); got != testCase.Out {
			t.Errorf("trim(%q) = %q, want %q", testCase.S, got, testCase.Out)
		}
	}
}

// referenceJSSlice is the straightforward UTF-16 encoding of the whole string,
// the implementation JSSlice shipped before the prefix fast path. The fast path
// must agree with it on every input (pi v1.5.0 utils/text.ts:48 ownedSlice,
// where V8's slice is O(1) in the returned length).
func referenceJSSlice(s string, start, end int) string {
	units := utf16.Encode([]rune(s))
	length := len(units)
	if start < 0 {
		start = max(length+start, 0)
	} else if start > length {
		start = length
	}
	if end < 0 {
		end = max(length+end, 0)
	} else if end > length {
		end = length
	}
	if start >= end {
		return ""
	}
	return string(utf16.Decode(units[start:end]))
}

// TestJSSliceMatchesFullEncoding pins the prefix fast path to the full UTF-16
// encode for every index combination the callers use.
func TestJSSliceMatchesFullEncoding(t *testing.T) {
	inputs := []string{
		"", "a", "abc", "héllo", "日本語", "a😀b", "😀", "e\u0301x",
		strings.Repeat("a", 300), strings.Repeat("日", 100), strings.Repeat("😀", 60),
		"\xff\xfe", "a\xed\xa0\x80b", "𝕳𝖊𝖑𝖑𝖔",
	}
	indices := []int{-5, -2, -1, 0, 1, 2, 3, 5, 50, 99, 100, 1000, 1 << 30}
	for _, in := range inputs {
		for _, start := range indices {
			for _, end := range indices {
				want := referenceJSSlice(in, start, end)
				got := JSSlice(in, start, end)
				if got != want {
					t.Fatalf("JSSlice(%q, %d, %d) = %q, want %q", in, start, end, got, want)
				}
			}
		}
	}
}

// TestJSSlicePrefixDoesNotScanTheWholeString guards the fix: a short prefix of a
// large body must not allocate in proportion to the body.
func TestJSSlicePrefixDoesNotScanTheWholeString(t *testing.T) {
	body := strings.Repeat("abcdefghij", 800000) // 8 MB
	const prefix = 500
	before := testing.AllocsPerRun(20, func() { _ = referenceJSSlice(body, 0, prefix) })
	allocs := testing.AllocsPerRun(20, func() {
		if got := JSSlice(body, 0, prefix); len(got) != prefix {
			t.Fatalf("len = %d, want %d", len(got), prefix)
		}
	})
	// The prefix must not allocate per byte; it may not even match the full
	// encoding, which allocates a 16 MB uint16 slice for the whole body.
	if allocs > before {
		t.Fatalf("JSSlice allocated %v times for a %d-char prefix of an 8 MB body, want <= %v", allocs, prefix, before)
	}
}

// Reference: ECMA-262 `\s` (WhiteSpace plus LineTerminator), measured against
// the pinned runtime with one code point at a time. Go's unicode.IsSpace and
// strings.TrimSpace disagree with it in both directions: they include U+0085
// (not JS whitespace) and miss U+FEFF (JS whitespace), so a port that swapped
// the class would change bash-mode detection, search tokenization and command
// argument trimming.
var jsSpaceRunes = []rune{
	0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x20,
	0xA0, 0x1680, 0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006,
	0x2007, 0x2008, 0x2009, 0x200A, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF,
}

func TestJSSpaceClassMatchesJavaScript(t *testing.T) {
	want := make(map[rune]bool, len(jsSpaceRunes))
	for _, r := range jsSpaceRunes {
		want[r] = true
	}
	for r := rune(0); r <= 0x10FFFF; r++ {
		if r >= 0xD800 && r <= 0xDFFF {
			continue
		}
		if got := IsJSSpace(r); got != want[r] {
			t.Fatalf("IsJSSpace(%04X) = %v; JavaScript \\s says %v", r, got, want[r])
		}
	}
	for _, notSpace := range []rune{0x00, 0x08, 0x1C, 0x85, 0x180E, 0x200B, 0x200C, 0x200D, 0x2060, 0x3001} {
		if IsJSSpace(notSpace) {
			t.Errorf("IsJSSpace(%04X) = true; JavaScript \\s does not include it", notSpace)
		}
	}
}

func TestJSTrimAndSplitUseJavaScriptWhitespace(t *testing.T) {
	for _, testCase := range []struct{ in, trimmedLeft, trimmed string }{
		{"\u00a0\u3000 x \ufeff", "x \ufeff", "x"},
		{"\u0085x\u0085", "\u0085x\u0085", "\u0085x\u0085"}, // U+0085 is not JS whitespace
		{"\u180exp", "\u180exp", "\u180exp"},                // U+180E stopped being whitespace in ES2016
		{"!cmd", "!cmd", "!cmd"},
		{"", "", ""},
	} {
		if got := JSTrimLeft(testCase.in); got != testCase.trimmedLeft {
			t.Errorf("JSTrimLeft(%q) = %q; want %q", testCase.in, got, testCase.trimmedLeft)
		}
		if got := JSTrim(testCase.in); got != testCase.trimmed {
			t.Errorf("JSTrim(%q) = %q; want %q", testCase.in, got, testCase.trimmed)
		}
	}

	for _, testCase := range []struct {
		in   string
		want []string
	}{
		{"  a\u00a0b\u3000c ", []string{"a", "b", "c"}},
		{"\ufeffone\ntwo", []string{"one", "two"}},
		{"a\u0085b", []string{"a\u0085b"}},
		{"", nil},
		{"   \t\n", nil},
	} {
		got := JSSplitWhitespace(testCase.in)
		if !slices.Equal(got, testCase.want) {
			t.Errorf("JSSplitWhitespace(%q) = %q; want %q", testCase.in, got, testCase.want)
		}
	}
}

// JSWhitespaceClass is built from the ECMAScript `\s` set; this pins its
// boundaries with the near misses that a truncated range would break. The set
// was verified once against every Unicode code point executed by the pinned
// engine (25 units: 0x9-0xd, 0x20, 0xa0, 0x1680, 0x2000-0x200a, 0x2028,
// 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff).
func TestJSWhitespaceClassBoundaries(t *testing.T) {
	class := regexp.MustCompile(`\A[` + JSWhitespaceClass + `]\z`)
	for _, unit := range []struct {
		code  rune
		space bool
	}{
		{0x08, false}, {0x09, true}, {0x0a, true}, {0x0b, true}, {0x0c, true}, {0x0d, true}, {0x0e, false},
		{0x20, true}, {0x1f, false}, {0x21, false}, {0x9f, false}, {0xa0, true}, {0xa1, false},
		{0x167f, false}, {0x1680, true}, {0x1681, false},
		{0x1fff, false}, {0x2000, true}, {0x200a, true}, {0x200b, false},
		{0x2027, false}, {0x2028, true}, {0x2029, true}, {0x202a, false},
		{0x202e, false}, {0x202f, true}, {0x2030, false},
		{0x205e, false}, {0x205f, true}, {0x2060, false},
		{0x2fff, false}, {0x3000, true}, {0x3001, false},
		{0xfefe, false}, {0xfeff, true}, {0xff00, false},
	} {
		if got := class.MatchString(string(unit.code)); got != unit.space {
			t.Errorf("U+%04X: in JavaScript \\s = %v, want %v", unit.code, got, unit.space)
		}
	}
	// The class is also what JSTrim trims, so the two must agree on those units.
	for _, code := range []rune{0x20, 0xa0, 0x1680, 0x2007, 0x2028, 0x202f, 0x205f, 0x3000, 0xfeff} {
		if !IsJSSpace(code) {
			t.Errorf("U+%04X: IsJSSpace = false, want true", code)
		}
	}
}

// JS String(value), captured from node: `String(v)` for each row. The reference
// wraps a thrown non-Error with `new Error(String(value))` in
// packages/mcp/src/protocol/jsonrpc.ts, so an object reports "[object Object]",
// an array joins with commas leaving null empty, and -0 reports "0".
func TestJSValueToString(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"null", nil, "null"},
		{"true", true, "true"},
		{"false", false, "false"},
		{"string", "s", "s"},
		{"empty string", "", ""},
		{"zero", float64(0), "0"},
		{"negative zero", math.Copysign(0, -1), "0"},
		{"fraction", 1.5, "1.5"},
		{"large", 1e21, "1e+21"},
		{"small", 1e-7, "1e-7"},
		{"int", 42, "42"},
		{"int64", int64(-7), "-7"},
		{"uint", uint(9), "9"},
		{"json number", json.Number("2.50"), "2.50"},
		{"empty array", []any{}, ""},
		{"null element", []any{nil}, ""},
		{"nested", []any{float64(1), []any{float64(2), "a"}, nil}, "1,2,a,"},
		{"empty object", map[string]any{}, "[object Object]"},
		{"object", map[string]any{"a": float64(1)}, "[object Object]"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := JSValueToString(test.value); got != test.want {
				t.Fatalf("JSValueToString(%#v) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

// The reference renders a caught value with `error instanceof Error ? error.message
// : String(error)` (packages/agent/src/agent-loop.ts, api/openai-completions.ts).
// A Go error keeps its identity; anything else goes through String(value).
func TestJSRecoveredError(t *testing.T) {
	sentinel := errors.New("boom")
	if got := JSRecoveredError(sentinel); got != sentinel {
		t.Fatalf("JSRecoveredError(error) = %v, want the same error value", got)
	}
	cases := []struct {
		name      string
		recovered any
		want      string
	}{
		{"object", map[string]any{"a": float64(1)}, "[object Object]"},
		{"slice", []any{"a", "b"}, "a,b"},
		{"string", "text", "text"},
		{"number", 42, "42"},
		{"nil", nil, "null"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := JSRecoveredError(test.recovered).Error(); got != test.want {
				t.Fatalf("JSRecoveredError(%#v) = %q, want %q", test.recovered, got, test.want)
			}
		})
	}
}
