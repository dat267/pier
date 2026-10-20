package coding

import "testing"

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
