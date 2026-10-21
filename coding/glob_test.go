package coding

import (
	"strings"
	"testing"
)

// Reference: pi v1.3.0 core/model-resolver.ts calls minimatch with
// { nocase: true }. Every expectation was read off minimatch 10.x with the same
// pattern, so `**` is a globstar only as a whole path segment, a wildcard never
// matches a leading dot, and a trailing globstar consumes at least one segment.
func TestMatchGlobAgainstMinimatch(t *testing.T) {
	cases := []struct {
		pattern string
		value   string
		want    bool
	}{
		{"*sonnet*", "claude-sonnet-4-5", true},
		{"*sonnet*", "anthropic/claude-sonnet-4-5", false},
		{"**sonnet**", "claude-sonnet-4-5", true},
		{"**sonnet**", "anthropic/claude-sonnet-4-5", false},
		{"a**b", "axb", true},
		{"a**b", "a/b", false},
		{"****", "a/b/c", false},
		{"***", "a/b", false},
		{"a/***", "a/b", true},
		{"a/***", "a/b/c", false},

		{"a/**", "a", false},
		{"a/**", "a/", true},
		{"a/**", "a//", true},
		{"a/**", "a/b/c", true},
		{"a/**", "a/.b", false},
		{"a/**/b", "a/b", true},
		{"a/**/b", "a/x/y/b", true},
		{"**/**", "a", true},
		{"**/**", "a/b", true},
		{"a/**/**", "a", false},
		{"a/**/**", "a/b", true},
		{"**/a", "a", true},
		{"**/a", "x/a", true},
		{"**/a/**", "a", false},
		{"**/a/**", "a/b", true},
		{"**/", "a/b", false},
		{"**/", "a", false},
		{"a/*/**", "a/b", false},
		{"a/*/**", "a/b/c", true},

		{"*", ".env", false},
		{"**", "a/.env", false},
		{"**", "a/b", true},
		{"**", "", true},
		{"**", "/", true},
		{"**", "./", false},
		{"*", "", false},
		{"*", "/", false},
		{"**/.env", "a/.env", true},
		{".*", ".env", true},
		{"?env", ".env", false},
		{"[!a]", ".", false},

		{"anthropic/*", "anthropic/claude-sonnet-4-5", true},
		{"anthropic/*", "openai/gpt-4o", false},
		{"{gpt-4o,sonnet}", "sonnet", true},
		{"gpt-4[ao]", "gpt-4o", true},

		// Repeated and leading separators, and absolute paths.
		{"a//b", "a/b", true},
		{"a//", "a/", true},
		{"b", "b/", true},
		{"a/", "a", false},
		{"*/", "a/b", false},
		{"*", "x/", true},
		{"/*", "//", false},
		{"*/**", "a/", true},
		{"/*", "/x", true},
		{"*", "/x", false},
		{"a", "/a", false},
		{"/**", "/a", true},
		{"/**", "a/b", false},
		{"**/x", "/x", true},
		{"**/", "a/b", false},
		{"a/*/**", "a/b", false},
	}
	for _, testCase := range cases {
		if got := MatchGlob(testCase.pattern, testCase.value, true); got != testCase.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", testCase.pattern, testCase.value, got, testCase.want)
		}
	}
}

// TestMatchGlobBraceSequencesAndDotSegments covers the shapes D262 used to
// list: a comma-less brace group is literal, `{1..3}`-style sequences expand
// with brace-expansion's padding and step rules, dot segments are matched
// literally, ".." removes the segment before it, and an empty path is one empty
// segment. Expectations read off minimatch 10.x.
func TestMatchGlobBraceSequencesAndDotSegments(t *testing.T) {
	cases := []struct {
		pattern string
		value   string
		want    bool
	}{
		{"{a}", "a", false},
		{"{a}", "{a}", true},
		{"{}", "{}", true},
		{"{a{b,c}}", "{ab}", true},
		{"{a{b,c}}", "ab", false},
		{"{1..3}", "2", true},
		{"{1..3}", "4", false},
		{"{3..1}", "2", true},
		{"{a..c}", "b", true},
		{"{01..03}", "02", true},
		{"{1..05}", "05", true},
		{"{1..05}", "5", false},
		{"{-1..1}", "0", true},
		{"{1..5..2}", "3", true},
		{"{a..c}{1..2}", "b2", true},

		{"a/../b", "b", true},
		{"a/../b", "a/b", false},
		{"a/..", "/", true},
		{"a/..", "", true},
		{"a/..", ".", false},
		{"a/b/..", "a", true},
		{"a/b/../../..", "..", true},
		{"/a/../b", "/b", true},
		{"/a/../b", "b", false},
		{".**/..", "//", true},

		{".*", ".", false},
		{".*", "..", false},
		{".*", "..a", true},
		{"?", ".", false},
		{"**/.", ".", true},
		{"**/..", "..", true},

		{"**/**", "", true},
		{"**/", "", true},
		{"**", "", true},
		{"**/*", "", false},
		{"*", "", false},
	}
	for _, testCase := range cases {
		if got := MatchGlob(testCase.pattern, testCase.value, true); got != testCase.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", testCase.pattern, testCase.value, got, testCase.want)
		}
	}
}

// Reference: pi v1.3.0 core/model-resolver.ts uses minimatch 10.2.6.
// brace-expansion counts the minus sign in the width of a padded number.
func TestMatchGlobNegativeBracePaddingIncludesSign(t *testing.T) {
	for _, testCase := range []struct {
		value string
		want  bool
	}{
		{"-01", true},
		{"-001", false},
		{"000", true},
		{"001", true},
	} {
		if got := MatchGlob("{-01..01}", testCase.value, false); got != testCase.want {
			t.Errorf("MatchGlob({-01..01}, %q) = %v, want %v", testCase.value, got, testCase.want)
		}
	}
}

// Reference: pi v1.3.0 core/model-resolver.ts -> minimatch 10.2.6.
// An escaped hyphen in a character class is literal, not a range operator.
func TestMatchGlobEscapedClassHyphenIsLiteral(t *testing.T) {
	for _, testCase := range []struct {
		value string
		want  bool
	}{
		{"a", true}, {"-", true}, {"c", true}, {"b", false},
	} {
		if got := MatchGlob(`[a\-c]`, testCase.value, false); got != testCase.want {
			t.Errorf("escaped class against %q = %v, want %v", testCase.value, got, testCase.want)
		}
	}
}

// Reference: pi v1.3.0 core/model-resolver.ts -> minimatch 10.2.6.
// brace-expansion preserves ${...} while still expanding later brace groups.
func TestMatchGlobDollarBracesStayLiteral(t *testing.T) {
	for _, testCase := range []struct {
		pattern, value string
		want           bool
	}{
		{"${a,b}", "${a,b}", true},
		{"${a,b}", "$a", false},
		{"${a,b}{1..2}", "${a,b}2", true},
		{"${a,b}{1..2}", "$a2", false},
	} {
		if got := MatchGlob(testCase.pattern, testCase.value, false); got != testCase.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", testCase.pattern, testCase.value, got, testCase.want)
		}
	}
}

// Reference: pi v1.3.0 core/model-resolver.ts -> minimatch's brace-expansion
// dependency uses balanced-match, which finds a balanced inner group even when
// an earlier opening brace is unmatched.
func TestMatchGlobUnclosedOuterBraceKeepsInnerExpansion(t *testing.T) {
	if !MatchGlob("{{a,b}", "{a", false) {
		t.Fatal("unmatched outer brace prevented the inner comma group from expanding")
	}
	if MatchGlob("{{a,b}", "{{a,b}", false) {
		t.Fatal("expandable inner group was treated as literal")
	}
}

// Reference: pi v1.3.0 core/model-resolver.ts -> minimatch 10.2.6.
// brace-expansion reparses a literal group followed by a comma and closing
// brace, allowing a later group to expand instead of stopping at the first.
func TestMatchGlobLiteralBraceBeforeCommaSet(t *testing.T) {
	for _, testCase := range []struct {
		pattern, value string
		want           bool
	}{
		{"{a},b}", "a}", true},
		{"{a},b}", "b", true},
		{"{a},b}", "{a},b}", false},
		{"{a}{b,c}", "{a}b", true},
		{"{a}{b,c}", "{a}c", true},
		{"{},a}b", "{},a}b", true},
		{"{},a}b", "ab", false},
	} {
		if got := MatchGlob(testCase.pattern, testCase.value, false); got != testCase.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", testCase.pattern, testCase.value, got, testCase.want)
		}
	}
}

// Reference: pi v1.3.0 core/model-resolver.ts -> minimatch 10.2.6.
// The singleton [.] class becomes a literal dot, including after another dot.
func TestMatchGlobLiteralDotClassesMatchParentSegment(t *testing.T) {
	for _, pattern := range []string{".[.]", "[.][.]"} {
		if !MatchGlob(pattern, "..", false) {
			t.Errorf("MatchGlob(%q, ..) = false, want true", pattern)
		}
	}
}

// Reference: pi v1.3.0 core/model-resolver.ts -> minimatch's brace-expansion
// uses JavaScript numbers. A sequence bound or step outside int64 must not
// turn into its first digit or overflow into a sequence in the other direction.
func TestMatchGlobLargeBraceSequenceNumbers(t *testing.T) {
	for _, testCase := range []struct {
		pattern, value string
		want           bool
	}{
		{"{1..3..-9223372036854775808}", "1", true},
		{"{1..3..-9223372036854775808}", "-9223372036854775807", false},
		{"{9223372036854775808..9223372036854775809}", "9223372036854776000", true},
		{"{9223372036854775808..9223372036854775809}", "9", false},
	} {
		if got := MatchGlob(testCase.pattern, testCase.value, false); got != testCase.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", testCase.pattern, testCase.value, got, testCase.want)
		}
	}
}

// Reference: pi v1.3.0 core/model-resolver.ts -> minimatch 10.2.6.
// When minimatch invokes brace-expansion, that layer consumes one of two
// backslashes before glob parsing. Without braces, minimatch skips that layer.
func TestMatchGlobBraceExpansionUnescapesBackslashPairs(t *testing.T) {
	for _, tc := range []struct {
		pattern, value string
		want           bool
	}{
		{`\\{a,b}`, "a", true},
		{`\\{a,b}`, `\a`, false},
		{`\\a`, `\a`, true},
		{`\\a`, "a", false},
		{`{a,\\{b,c}}`, "b", true},
	} {
		if got := MatchGlob(tc.pattern, tc.value, false); got != tc.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", tc.pattern, tc.value, got, tc.want)
		}
	}
}

// Reference: pi v1.3.0 core/model-resolver.ts -> minimatch 10.2.6 skips
// brace-expansion when no brace group is present. A backslash pair then protects
// itself, rather than letting its second slash escape a following dot.
func TestMatchGlobBackslashPairBeforeLiteralDot(t *testing.T) {
	if !MatchGlob(`\\..\,a`, `\..,a`, false) {
		t.Fatal("backslash pair was consumed as an escape for the following dot")
	}
}

// Reference: pi v1.3.0 core/model-resolver.ts -> minimatch 10.2.6.
// An alphabetic range crossing backslash contains an empty member. Whether it
// survives is decided by the first group, not reset by a later sequence.
func TestMatchGlobFirstBraceGroupControlsEmptyResults(t *testing.T) {
	if MatchGlob("{,}{Z..a}", "", false) {
		t.Fatal("later sequence kept an empty result discarded by the leading comma group")
	}
	if !MatchGlob("{Z..a}", "", false) {
		t.Fatal("leading alphabetic sequence must keep its empty backslash member")
	}
}

// Reference: pi v1.3.0 core/model-resolver.ts -> minimatch's brace-expansion
// stops when a JavaScript sequence comparison becomes false, including NaN
// produced by adding opposite infinities.
func TestMatchGlobInfiniteBraceStepDoesNotProduceNaN(t *testing.T) {
	huge := strings.Repeat("9", 400)
	pattern := "{-" + huge + "..1.." + huge + "}"
	if MatchGlob(pattern, "NaN", false) {
		t.Fatal("sequence emitted NaN after its numeric comparison became false")
	}
	if !MatchGlob(pattern, "-Infinity", false) {
		t.Fatal("sequence lost its initial infinite bound")
	}
}

// Reference: pi v1.3.0 core/model-resolver.ts -> brace-expansion's comma-set
// rewrite uses a JavaScript dot without dotAll, so no JS line terminator may
// be crossed while looking for the later closing brace.
func TestMatchGlobBraceRewriteDoesNotCrossLineTerminators(t *testing.T) {
	for _, separator := range []string{"\n", "\r", "\u2028", "\u2029"} {
		pattern := "{a}," + separator + "}"
		if !MatchGlob(pattern, pattern, false) {
			t.Errorf("literal pattern %q was rewritten across a line terminator", pattern)
		}
	}
}

// Reference: pi v1.3.0 core/model-resolver.ts -> minimatch 10.2.6 parseClass.
// Out-of-order ranges are discarded. If none of the class remains, it matches
// nothing even when negated, rather than becoming a universal character class.
func TestMatchGlobInvalidNegatedRangeMatchesNothing(t *testing.T) {
	for _, tc := range []struct {
		pattern, value string
		want           bool
	}{
		{"[!z-a]", "x", false},
		{"[^z-a]", "x", false},
		{"[!z-aq]", "x", true},
		{"[!z-aq]", "q", false},
	} {
		if got := MatchGlob(tc.pattern, tc.value, false); got != tc.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", tc.pattern, tc.value, got, tc.want)
		}
	}
}

// TestMatchGlobEscapedSuffixFastPath pins D262's one remaining difference:
// minimatch's fast path for "*<literal>" and "?<literal>" patterns compares the
// raw escape text, so `*\}` does not match "}", while the port (and minimatch's
// own compiled regex) matches the escaped character.
func TestMatchGlobEscapedSuffixFastPath(t *testing.T) {
	cases := []struct{ pattern, value string }{
		{`*\}`, "}"},
		{`x*\}`, "x}"},
		{`?\}`, "x}"},
		{`*\}`, "2.{}"},
	}
	for _, testCase := range cases {
		if !MatchGlob(testCase.pattern, testCase.value, true) {
			t.Errorf("MatchGlob(%q, %q) = false; the port matches the escaped character", testCase.pattern, testCase.value)
		}
	}
}
