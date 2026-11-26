package tui

import (
	"encoding/json"
	"os"
	"testing"
)

// Upstream's cjkPunctuationRegex matches punctuation that is itself a
// CJK-script character, or one of the explicit list; the lookahead does not
// involve the next character.
func TestAutocompleteSeparatorMatchesCJKPunctuation(t *testing.T) {
	cases := []struct {
		char rune
		want bool
	}{
		{'。', true},
		{'、', true},
		{'「', true},
		{'」', true},
		{'，', true},
		{'．', true},
		{' ', true},
		{'\t', true},
		{'.', false},
		{'中', false},
		{'a', false},
	}
	for _, testCase := range cases {
		if got := isAutocompleteSeparator(testCase.char); got != testCase.want {
			t.Errorf("isAutocompleteSeparator(%q) = %v; want %v", testCase.char, got, testCase.want)
		}
	}
}

func TestAutocompleteTokenBoundaryCJKPunctuation(t *testing.T) {
	// ASCII punctuation before a CJK character is not a boundary.
	if got := findLastDelimiter("a.中"); got != -1 {
		t.Errorf("findLastDelimiter(%q) = %d; want -1", "a.中", got)
	}
	// A trailing CJK punctuation character is a boundary.
	if !tokenStartMatches("a。") {
		t.Error("tokenStartMatches(\"a。\") = false; want true")
	}
	if tokenStartMatches("a中") {
		t.Error("tokenStartMatches(\"a中\") = true; want false")
	}
}

// Upstream's buildTriggerPattern allows an opening bracket/quote/backtick
// between the boundary and the trigger character.
func TestEditorTriggerMatchAllowsBracketPrefix(t *testing.T) {
	triggers := []string{"@", "#"}
	cases := []struct {
		text string
		want bool
	}{
		{"@", true},
		{"(@", true},
		{"[@", true},
		{"`@", true},
		{"{@", true},
		{"<@", true},
		{"(@src/foo", true},
		{`@"`, true},
		{`(@"`, true},
		{`@"foo`, true},
		{`@"foo bar`, true},
		{`(@"foo bar`, true},
		{`@"foo"`, true},
		{`@"foo bar"`, false},
		{`(@"foo`, true},
		{"x@", false},
		{"a(@", false},
		{"@src/foo bar", false},
	}
	for _, testCase := range cases {
		if got := editorTriggerMatch(testCase.text, triggers); got != testCase.want {
			t.Errorf("editorTriggerMatch(%q) = %v; want %v", testCase.text, got, testCase.want)
		}
	}
}

// Upstream's isTokenStart strips opening wrappers before checking the boundary.
func TestExtractQuotedPrefixAllowsWrapper(t *testing.T) {
	cases := []struct {
		text string
		want string
		ok   bool
	}{
		{`"foo`, `"foo`, true},
		{`(@"foo`, `@"foo`, true},
		{`[@"foo`, `@"foo`, true},
		{`@"foo`, `@"foo`, true},
		{`x"foo`, "", false},
	}
	for _, testCase := range cases {
		got, ok := extractQuotedPrefix(testCase.text)
		if got != testCase.want || ok != testCase.ok {
			t.Errorf("extractQuotedPrefix(%q) = (%q, %v); want (%q, %v)", testCase.text, got, ok, testCase.want, testCase.ok)
		}
	}
}

// Upstream strips opening wrappers before a path token in extractAtPrefix and
// extractPathPrefix (stripLeadingWrappers).
func TestExtractPrefixStripsWrappers(t *testing.T) {
	provider := &CombinedAutocompleteProvider{}
	if got, ok := provider.extractAtPrefix("(@src"); !ok || got != "@src" {
		t.Errorf("extractAtPrefix(%q) = (%q, %v); want (%q, true)", "(@src", got, ok, "@src")
	}
	if got, ok := provider.extractPathPrefix("(@src/foo", false); !ok || got != "@src/foo" {
		t.Errorf("extractPathPrefix(%q) = (%q, %v); want (%q, true)", "(@src/foo", got, ok, "@src/foo")
	}
	if got, ok := provider.extractPathPrefix("(~/Dev", false); !ok || got != "~/Dev" {
		t.Errorf("extractPathPrefix(%q) = (%q, %v); want (%q, true)", "(~/Dev", got, ok, "~/Dev")
	}
	if got, ok := provider.extractPathPrefix("(group)/pa", false); !ok || got != "(group)/pa" {
		t.Errorf("extractPathPrefix(%q) = (%q, %v); want (%q, true)", "(group)/pa", got, ok, "(group)/pa")
	}
}

// The hand-enumerated cjkPunctuationRanges table claims to be the reference's
// (punctuation ∩ CJK-script) set. This replays the reference's own answer for
// every code point, generated from the pinned utils.ts by
// tui/testdata/autocomplete-separator/generate.mjs, so a Unicode table shift in
// Go or a typo in the enumeration fails here instead of in a user's completion.
func TestAutocompleteSeparatorMatchesTheReference(t *testing.T) {
	data, err := os.ReadFile("testdata/autocomplete-separator/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Count  int       `json:"count"`
		Ranges [][2]rune `json:"ranges"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	want := make(map[rune]bool, fixture.Count)
	for _, pair := range fixture.Ranges {
		for codePoint := pair[0]; codePoint <= pair[1]; codePoint++ {
			want[codePoint] = true
		}
	}
	if len(want) != fixture.Count {
		t.Fatalf("fixture holds %d code points, count says %d", len(want), fixture.Count)
	}
	var extra, missing []rune
	for codePoint := rune(0); codePoint <= 0x10ffff; codePoint++ {
		if codePoint >= 0xd800 && codePoint <= 0xdfff {
			continue
		}
		got := isAutocompleteSeparator(codePoint)
		if got && !want[codePoint] {
			extra = append(extra, codePoint)
		}
		if !got && want[codePoint] {
			missing = append(missing, codePoint)
		}
	}
	if len(extra) > 0 || len(missing) > 0 {
		t.Fatalf("separator mismatch: extra %v, missing %v (regenerate the fixture or fix cjkPunctuationRanges)",
			extra, missing)
	}
}

// TestAutocompleteSeparatorTableIsExact checks the TABLE, not the predicate.
// TestAutocompleteSeparatorMatchesTheReference above compares
// isAutocompleteSeparator, and unicode.IsPunct masks an over-wide range for the
// Symbol code points inside it: {0x3008,0x301D} spans U+3012/U+3013 (So, not Po),
// which the reference excludes, and the predicate still said false for them. A
// future caller using the table alone would have separated on those two.
func TestAutocompleteSeparatorTableIsExact(t *testing.T) {
	// Copied from testdata/autocomplete-separator/golden.json, which the generator
	// derives from the pinned utils.ts.
	want := [][2]rune{
		{0x00B7, 0x00B7}, {0x3001, 0x3003}, {0x3008, 0x3011}, {0x3014, 0x301F},
		{0x3030, 0x3030}, {0x303D, 0x303D}, {0x30A0, 0x30A0}, {0x30FB, 0x30FB},
		{0xFE45, 0xFE46}, {0xFF61, 0xFF65}, {0x16FE2, 0x16FE2},
	}
	if len(cjkPunctuationRanges) != len(want) {
		t.Fatalf("table has %d ranges, want %d", len(cjkPunctuationRanges), len(want))
	}
	for i := range want {
		if cjkPunctuationRanges[i] != want[i] {
			t.Errorf("range %d = {0x%04X,0x%04X}, want {0x%04X,0x%04X}", i,
				cjkPunctuationRanges[i][0], cjkPunctuationRanges[i][1], want[i][0], want[i][1])
		}
	}
}
