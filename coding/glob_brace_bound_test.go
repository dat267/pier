package coding

import (
	"strings"
	"testing"
	"time"
)

// TestExpandBracesIsBounded covers brace-expansion's limits (the CVE-2026-14257
// fix): at most EXPANSION_MAX results and EXPANSION_MAX_LENGTH total characters,
// so a pattern with many brace groups cannot exhaust memory. minimatch 10.x
// ships those limits; the port expanded every combination.
func TestExpandBracesIsBounded(t *testing.T) {
	many := strings.Repeat("{a,b}", 20)
	expanded := expandBraces(many)
	if len(expanded) > 100_000 {
		t.Fatalf("expandBraces(%d groups) produced %d results", strings.Count(many, "{"), len(expanded))
	}
	if len(expanded) == 0 {
		t.Fatal("no expansions produced")
	}

	long := strings.Repeat("{a,b}", 1500)
	expanded = expandBraces(long)
	total := 0
	for _, entry := range expanded {
		total += len(entry)
	}
	if total > 4_000_000+len(long) {
		t.Fatalf("expandBraces accumulated %d characters", total)
	}

	// An ordinary pattern still expands fully.
	if got := strings.Join(expandBraces("a{b,c}d{e,f}"), ","); got != "abde,abdf,acde,acdf" {
		t.Fatalf("ordinary expansion = %q", got)
	}
}

// TestMatchGlobEscapedBraces covers brace-expansion's escapes: a backslash
// protects the following brace or comma, so it stays literal. Expectations read
// off minimatch 10.x (braceExpand + minimatch with nocase).
func TestMatchGlobEscapedBraces(t *testing.T) {
	cases := []struct {
		pattern string
		value   string
		want    bool
	}{
		{`\{a,b\}`, "{a,b}", true},
		{`\{a,b\}`, "a", false},
		{`a\{b,c\}d`, "a{b,c}d", true},
		{`{a\,b,c}`, "a,b", true},
		{`{a\,b,c}`, "c", true},
		{`\{a\}`, "{a}", true},
	}
	for _, testCase := range cases {
		if got := MatchGlob(testCase.pattern, testCase.value, false); got != testCase.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", testCase.pattern, testCase.value, got, testCase.want)
		}
	}
}

// TestMatchGlobBacktrackingIsMemoized covers a pattern of many "*a" segments
// that fails to match: a naive per-star recursion backtracks exponentially
// (2^stars) and hangs, while the memoized segment match is polynomial. The
// reference answers false (there is no "b" in the value).
func TestMatchGlobBacktrackingIsMemoized(t *testing.T) {
	pattern := strings.Repeat("*a", 60) + "*b"
	value := strings.Repeat("a", 200)
	done := make(chan bool, 1)
	go func() { done <- MatchGlob(pattern, value, true) }()
	select {
	case got := <-done:
		if got {
			t.Fatalf("MatchGlob(%q, %q) = true, want false", pattern, value)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("MatchGlob(%q, %q) did not finish", pattern, value)
	}
}
