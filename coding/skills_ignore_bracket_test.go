package coding

import (
	"testing"
)

// pi v1.5.0 (f9a26365) pins node_modules/ignore, whose `scanBracket` reads a
// bracket expression with the grammar of git's wildmatch.c: a leading `!` or
// `^` negates, a leading `]` is a member, `[:name:]` is one of the twelve POSIX
// ASCII classes, an out-of-order range keeps its lower bound, and an
// unterminated expression (or an unknown POSIX class) makes the whole pattern
// match nothing. A bracket expression never matches a path separator.
//
// These cases were measured from the pinned package.
func TestSkillIgnoreBracketExpressionsMatchNodeIgnore(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		ignored bool
	}{
		// A leading `^` negates exactly like `!`; the old translation read it
		// as the literal member `^`, which inverted the rule.
		{"[^a]", "a", false},
		{"[^a]", "b", true},
		{"[^a]", "^", true},
		{"[^a]", "x/y", true},
		{"[!a]", "a", false},
		{"[!a]", "b", true},
		{"[^a-c]", "d", true},
		{"[^a-c]", "b", false},
		// POSIX ASCII classes.
		{"[[:alpha:]]", "a", true},
		{"[[:alpha:]]", "Z", true},
		{"[[:alpha:]]", "1", false},
		{"[[:digit:]]", "1", true},
		{"[[:digit:]]", "a", false},
		{"[[:upper:]]", "A", true},
		{"[[:upper:]]", "a", true}, // node-ignore compiles with the `i` flag
		{"[[:lower:]]", "a", true},
		{"[[:alnum:]]", "9", true},
		{"[[:space:]]", " ", true},
		{"[[:punct:]]", "!", true},
		{"[[:xdigit:]]", "f", true},
		{"[[:blank:]]", "\t", true},
		{"[[:alpha:]0-9]", "7", true},
		{"[a[:digit:]z]", "5", true},
		// A `]` right after `[` or `[!` is a member, not the terminator.
		{"[]]", "]", true},
		{"[]]", "a", false},
		{"[!]]", "a", true},
		{"[!]]", "]", false},
		// An out-of-order range matches its lower bound only.
		{"[c-a]", "c", true},
		{"[c-a]", "a", false},
		// Escaped members.
		{"[\\]]", "]", true},
		{"[\\-]", "-", true},
		{"[--0]", "-", true},
		{"[--0]", ".", true},
		{"[--0]", "0", true},
		{"[--0]", "1", false},
		// A bracket expression never matches a path separator.
		{"[a/b]", "x/y", false},
		{"[!-~]", "x/y", true},
		// Members that look like wildcards are literal.
		{"[*]", "*", true},
		{"[?]", "?", true},
	}
	for _, c := range cases {
		rule, ok := parseSkillIgnoreRule(c.pattern, "")
		if !ok {
			t.Errorf("%q: rule dropped", c.pattern)
			continue
		}
		if got := skillPathIgnored(c.path, false, []skillIgnoreRule{rule}); got != c.ignored {
			t.Errorf("%q on %q = %v, want %v", c.pattern, c.path, got, c.ignored)
		}
	}
}

// TestSkillIgnoreUnterminatedBracketMatchesNothing pins the wildmatch
// `WM_ABORT_ALL` outcome: a pattern whose bracket expression never closes
// matches no path at all.
func TestSkillIgnoreUnterminatedBracketMatchesNothing(t *testing.T) {
	for _, pattern := range []string{"[alpha", "a[b", "[[:nosuch:]]*", "[[:alpha:]*"} {
		rule, ok := parseSkillIgnoreRule(pattern, "")
		if !ok {
			continue // Dropping the rule is the same outcome as matching nothing.
		}
		for _, path := range []string{"a", "alpha", "[alpha", "b", "z"} {
			if skillPathIgnored(path, true, []skillIgnoreRule{rule}) {
				t.Errorf("%q ignored %q; an aborted pattern matches nothing", pattern, path)
			}
		}
	}
}

// TestSkillIgnoreMatchesCaseInsensitivelyWithoutFoldingThePattern pins the
// `i`-flag half of the port: node-ignore compares raw characters when it
// decides whether a `-` opens a range, so `[a-Z]` drops the reversed range and
// keeps only `a`, and the `i` flag then also matches `A`. Folding the pattern
// to lower case first turned `[a-Z]` into `[a-z]`, which matched every letter.
func TestSkillIgnoreMatchesCaseInsensitivelyWithoutFoldingThePattern(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		ignored bool
	}{
		{"[a-Z]", "a", true},
		{"[a-Z]", "A", true},
		{"[a-Z]", "m", false},
		{"[a-Z]", "Z", false},
		{"[z-a]", "z", true},
		{"[z-a]", "a", false},
		{"[A-z]", "m", true},
		{"[M-P]", "n", true},
		{"[[:upper:]]", "m", true},
		{"[[:lower:]]", "M", true},
	}
	for _, c := range cases {
		rule, ok := parseSkillIgnoreRule(c.pattern, "")
		if !ok {
			t.Errorf("%q: rule dropped", c.pattern)
			continue
		}
		if got := skillPathIgnored(c.path, false, []skillIgnoreRule{rule}); got != c.ignored {
			t.Errorf("%q on %q = %v, want %v", c.pattern, c.path, got, c.ignored)
		}
	}
}
