package tui

// Upstream latex.ts normalizes a script value with
// `value.replace(/\s*([=+-])\s*/g, "$1")`; pier scans bytes instead of running
// the regexp, because the backtracker dominated display renders (D466). This
// test keeps the scan equal to the regexp over every byte combination the
// transform can see, including the whitespace `\s` accepts and the Unicode
// spaces it does not.

import (
	"strings"
	"testing"
)

func TestNormalizeScriptValueMatchesTheRelationRegex(t *testing.T) {
	reference := func(value string) string {
		return latexScriptRelationRegex.ReplaceAllString(value, "${1}")
	}
	alphabet := []string{"", " ", "  ", "\t", "\n", "\r", "\f", "\v", "a", "=", "+", "-", "*", "é"}
	check := func(value string) {
		t.Helper()
		want := reference(strings.TrimSpace(value))
		if got := normalizeScriptValue(value); got != want {
			t.Fatalf("normalizeScriptValue(%q) = %q, regexp gives %q", value, got, want)
		}
	}
	for _, a := range alphabet {
		check(a)
		for _, b := range alphabet {
			check(a + b)
			for _, c := range alphabet {
				check(a + b + c)
				for _, d := range alphabet {
					check(a + b + c + d)
				}
			}
		}
	}
	for _, value := range []string{
		"a + b + c",
		"a  =  b",
		"n = 1",
		"a + + b",
		"a = - b",
		"  x  ",
		"\v= \v",
		"a\v= b",
		"+++",
		"i+1",
		"1 + 1 = 2",
	} {
		check(value)
	}
}

// TestCollapseWhitespaceRunsMatchesTheWhitespaceRegex pins the byte scan to the
// regexp upstream uses for a layout line, including the whitespace the pattern
// leaves alone (newline, vertical tab, Unicode spaces).
func TestCollapseWhitespaceRunsMatchesTheWhitespaceRegex(t *testing.T) {
	reference := func(value string) string {
		return latexWhitespaceRunRegex.ReplaceAllString(value, " ")
	}
	check := func(value string) {
		t.Helper()
		if got, want := collapseWhitespaceRuns(value), reference(value); got != want {
			t.Fatalf("collapseWhitespaceRuns(%q) = %q, regexp gives %q", value, got, want)
		}
	}
	alphabet := []string{"", " ", "\t", "\n", "\v", "\r", "a", "=", "é", "\u00a0"}
	for _, a := range alphabet {
		check(a)
		for _, b := range alphabet {
			check(a + b)
			for _, c := range alphabet {
				check(a + b + c)
				for _, d := range alphabet {
					check(a + b + c + d)
				}
			}
		}
	}
	for _, value := range []string{" a  b ", "\t\ta\tb\t\t", "a\t \t b", "  ", "\n\n", "x  \n  y"} {
		check(value)
	}
}
