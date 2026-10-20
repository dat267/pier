package coding

import "testing"

// FuzzMatchGlob checks that pattern matching never panics and that the answer is
// stable across calls. The matcher, its brace expansion and its escape handling
// all take user input, and a malformed pattern must not crash the process. (A
// case-insensitive match is not a superset of the case-sensitive one: lowering
// the value can move it into a negated class, as "[!a]*" against "A" shows.)
func FuzzMatchGlob(f *testing.F) {
	seeds := [][2]string{
		{"*sonnet*", "claude-sonnet-4-5"},
		{"**/x", "/x"},
		{"a/**/b", "a/b"},
		{"{a,b}{c,d}", "bd"},
		{`\{a,b\}`, "{a,b}"},
		{`{a\,b,c}`, "a,b"},
		{"[!a]*", "abc"},
		{"a**b", "a/b"},
		{"***", "a/b"},
		{"", ""},
		{"/**", "/a"},
		{"**sonnet**", "anthropic/claude-sonnet-4-5"},
	}
	for _, seed := range seeds {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, pattern, value string) {
		first := MatchGlob(pattern, value, true)
		if second := MatchGlob(pattern, value, true); second != first {
			t.Fatalf("MatchGlob(%q, %q) is not deterministic: %v then %v", pattern, value, first, second)
		}
		_ = MatchGlob(pattern, value, false)
	})
}
