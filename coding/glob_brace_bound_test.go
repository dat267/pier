package coding

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

// Reference: pi v1.4.0 core/model-resolver.ts uses minimatch 10.2.6;
// brace-expansion's combine caps results before growing its output array.
// A modest Cartesian product catches reserving the entire product without
// reproducing the 160 GB allocation of {1..100000}{1..100000}.
func TestMatchGlobBraceProductAllocationIsBounded(t *testing.T) {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	matched := MatchGlob("{1..2048}{1..2048}", "11", false)
	runtime.ReadMemStats(&after)
	if !matched {
		t.Fatal("first brace expansion must match")
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 16<<20 {
		t.Fatalf("brace product allocated %d bytes, want at most 16 MiB", allocated)
	}
}

// Reference: pi v1.4.0 core/model-resolver.ts -> minimatch -> brace-expansion.
// Reaching the expansion limit truncates alternatives, not the rest of each
// pattern: the retained alternatives still include their literal suffix.
func TestMatchGlobTruncatedBraceExpansionKeepsSuffix(t *testing.T) {
	pattern := "{1..100001}x"
	if !MatchGlob(pattern, "1x", false) {
		t.Fatal("retained expansion lost its suffix")
	}
	if MatchGlob(pattern, "1", false) {
		t.Fatal("truncated expansion matched without its required suffix")
	}
}

// Reference: pi v1.4.0 core/model-resolver.ts -> minimatch -> brace-expansion.
// The intermediate list of comma alternatives is bounded as well as the final
// Cartesian product; otherwise many sequence alternatives bypass the budget.
func TestMatchGlobNestedBraceAlternativesAllocationIsBounded(t *testing.T) {
	pattern := "{" + strings.TrimSuffix(strings.Repeat("{1..4096},", 64), ",") + "}"
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	matched := MatchGlob(pattern, "1", false)
	runtime.ReadMemStats(&after)
	if !matched {
		t.Fatal("first nested alternative must match")
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 24<<20 {
		t.Fatalf("nested alternatives allocated %d bytes, want at most 24 MiB", allocated)
	}
}

// Reference: pi v1.4.0 core/model-resolver.ts -> minimatch. A literal mismatch
// needs no backtracking states; reserving patternLen*valueLen map entries
// before inspecting the first character makes a short input exhaust memory.
func TestMatchGlobLiteralMismatchAllocationIsLinear(t *testing.T) {
	pattern, value := strings.Repeat("a", 1024), strings.Repeat("b", 1024)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	matched := MatchGlob(pattern, value, false)
	runtime.ReadMemStats(&after)
	if matched {
		t.Fatal("distinct literal strings matched")
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 1<<20 {
		t.Fatalf("literal mismatch allocated %d bytes, want at most 1 MiB", allocated)
	}
}

// Reference: pi v1.4.0 core/model-resolver.ts -> minimatch. The same bound
// applies across path segments: a mismatch at the first segment must not
// reserve every possible pair of subsequent segment offsets.
func TestMatchGlobPathMismatchAllocationIsLinear(t *testing.T) {
	pattern, value := strings.Repeat("a/", 1024), strings.Repeat("b/", 1024)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	matched := MatchGlob(pattern, value, false)
	runtime.ReadMemStats(&after)
	if matched {
		t.Fatal("distinct literal paths matched")
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 1<<20 {
		t.Fatalf("path mismatch allocated %d bytes, want at most 1 MiB", allocated)
	}
}

// Reference: pi v1.4.0 core/model-resolver.ts -> minimatch -> brace-expansion.
// Top-level empty alternatives are discarded before counting against the
// retained-result limit, so they cannot hide a later non-empty alternative.
func TestMatchGlobDiscardedEmptyAlternativesDoNotUseBudget(t *testing.T) {
	pattern := "{" + strings.Repeat(",", globBraceExpansionMax) + "a}"
	if !MatchGlob(pattern, "a", false) {
		t.Fatal("discarded empty alternatives exhausted the result budget")
	}
}

// Reference: pi v1.4.0 core/model-resolver.ts -> minimatch 10.2.6 with
// brace-expansion 5.0.12. Beyond EXPANSION_MAX_DEPTH (1000), nested groups stay
// literal instead of consuming unbounded parser stack and repeated scans.
func TestMatchGlobDeepBraceNestingStaysLiteral(t *testing.T) {
	pattern := strings.Repeat("{", 1002) + "a,b" + strings.Repeat("}", 1002)
	if !MatchGlob(pattern, pattern, false) {
		t.Fatal("brace nesting beyond the reference depth limit did not stay literal")
	}
}

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
