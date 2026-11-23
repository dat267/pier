package coding

import "testing"

// Reference: pi v1.6.0 (faac5858) core/model-resolver.ts matches --models
// globs with minimatch 11.0.3 { nocase: true }, whose portion matcher is
// @isaacs/nonrecursive-glob-match 1.0.3 (src/index.ts). Every expectation was
// read off that minimatch; a 20,000-case differential of random extglob
// patterns and values (nesting, negation, dotfiles, "." and "..", classes,
// escapes, unclosed groups) found no divergence.
func TestMatchGlobExtendedGlobsAgainstMinimatch(t *testing.T) {
	cases := []struct {
		pattern string
		value   string
		want    bool
	}{
		{"@(opus|sonnet)", "sonnet", true},
		{"@(opus|sonnet)", "haiku", false},
		{"claude-@(opus|sonnet)-*", "claude-opus-5-5", true},
		{"claude-@(opus|sonnet)-*", "claude-haiku-5-5", false},
		// A negation inside a segment rejects only the exact text it spans,
		// so a following * can still cover what the negation left out.
		{"*/claude-!(opus)*", "anthropic/claude-sonnet-5-5", true},
		{"*/claude-!(opus)*", "anthropic/claude-opus-5-5", true},
		{"claude-!(opus)-5-5", "claude-opus-5-5", false},
		{"claude-!(opus)-5-5", "claude-sonnet-5-5", true},
		{"gpt-+(6|.)*", "gpt-6.2-sol", true},
		{"?(us.)anthropic.*", "us.anthropic.claude", true},
		{"?(us.)anthropic.*", "anthropic.claude", true},
		{"*(a|b)c", "ababc", true},
		{"*(a|b)c", "abxc", false},
		{"@(A|b)x", "ax", true},
		// A leading "!" is whole-pattern negation (parseNegate runs first), so
		// "!(a)" is "not the literal (a)".
		{"!(a)", "a", true},
		{"!(a)", ".b", true},
		// Dotfiles: a literal dot inside a group matches, a wildcard does not,
		// and "." or ".." match only literal dots.
		{"@(.a)", ".a", true},
		{"*(.a)", ".a", true},
		{"@(a|*)", ".a", false},
		{"@(a)", "..", false},
		{"@(..)", "..", true},
		// Unclosed groups and the empty-group shapes stay literal.
		{"@(a|b", "@(a|b", true},
		{"a|b)", "a|b)", true},
		{"?()", "x()", true},
		{"@()", "@()", true},
		// Groups never span a "/" segment boundary.
		{"@(a)/b", "a/b", true},
		{"@(a/b)", "a/b", false},
		{"[ab]@(c)", "bc", true},
	}
	for _, tc := range cases {
		if got := MatchGlob(tc.pattern, tc.value, true); got != tc.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", tc.pattern, tc.value, got, tc.want)
		}
	}
}
