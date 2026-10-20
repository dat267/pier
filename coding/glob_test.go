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

// TestMatchGlobResidualMinimatchQuirks pins D262's remaining differences, which
// a differential over 50,000 generated pattern/value pairs found. Model ids are
// never "." / "..", are never empty, and their scoped patterns do not use a
// single-alternative brace, so the port keeps the simpler behavior here.
func TestMatchGlobResidualMinimatchQuirks(t *testing.T) {
	cases := []struct {
		pattern string
		value   string
		port    bool
	}{
		// minimatch treats a comma-less brace group as literal text.
		{"{a}", "a", true},
		// minimatch never matches the "." or ".." path segments with a pattern
		// that starts with a dot wildcard.
		{".*", ".", true},
		{".*", "..", true},
		// minimatch matches an empty path against a pattern of globstar segments.
		{"**/**", "", false},
		{"**/", "", false},
	}
	for _, testCase := range cases {
		if got := MatchGlob(testCase.pattern, testCase.value, true); got != testCase.port {
			t.Errorf("MatchGlob(%q, %q) = %v, want the documented %v", testCase.pattern, testCase.value, got, testCase.port)
		}
	}
}
