package coding

import "testing"

// FuzzMatchGlob checks that pattern matching never panics and that the
// case-insensitive answer is a superset of the case-sensitive one (lowercasing
// can only turn a mismatch into a match, never the reverse). The matcher, its
// brace expansion and its escape handling all take user input.
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
		sensitive := MatchGlob(pattern, value, false)
		insensitive := MatchGlob(pattern, value, true)
		if sensitive && !insensitive {
			t.Fatalf("MatchGlob(%q, %q): case-sensitive matches but case-insensitive does not", pattern, value)
		}
	})
}
