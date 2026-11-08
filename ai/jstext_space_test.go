package ai

import (
	"slices"
	"testing"
)

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
