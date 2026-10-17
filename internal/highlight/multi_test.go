package highlight

import (
	"strings"
	"testing"
)

// TestPrefixedDispatchKeepsDotInsideOneLine guards the anchored dispatch
// matcher: it consumes the real preceding byte with `(?s:.)`, and that DOTALL
// must not leak into the rules. A leaked `s` flag makes a rule's `.` cross
// newlines, so EBNF's `\?.*\?` special sequence would swallow every later line
// instead of closing on the same line.
func TestPrefixedDispatchKeepsDotInsideOneLine(t *testing.T) {
	result, err := Highlight("a = ? x ? ;\nb = ? y ? ;\n", "ebnf")
	if err != nil {
		t.Fatalf("highlight: %v", err)
	}
	html := RenderHTML(result.Nodes)
	if got := strings.Count(html, `class="hljs-meta"`); got != 2 {
		t.Fatalf("got %d meta spans, want 2; a rule's `.` crossed newlines:\n%s", got, html)
	}
}

// TestIgnoredZeroWidthEndAdvances guards the scan loop's handling of an ignored
// zero-width end. julia-repl ends its sub-language at `^(?![ ]{6})`, ported as a
// zero-width `^` end with an onEnd guard; returning 0 would re-find the same end
// and burn the iteration cap while swallowing the rest of the input.
func TestIgnoredZeroWidthEndAdvances(t *testing.T) {
	result, err := Highlight("julia> foo\n      bar\nbaz\n", "julia-repl")
	if err != nil {
		t.Fatalf("highlight: %v", err)
	}
	html := RenderHTML(result.Nodes)
	if !strings.Contains(html, "</span>baz") {
		t.Fatalf("the unindented line was swallowed by the REPL block:\n%s", html)
	}
}

// TestMidLineBeginLineDoesNotMatch guards the non-dispatch scan path's context
// verification: a `^` rule must not match mid-line just because the scan slice
// starts there. armasm's `^[ \t]*(?=#)` comment rule only matches a line-leading
// `#`, so `mov r0, #1` keeps `#1` as a number.
func TestMidLineBeginLineDoesNotMatch(t *testing.T) {
	result, err := Highlight("mov r0, #1\n", "armasm")
	if err != nil {
		t.Fatalf("highlight: %v", err)
	}
	if html := RenderHTML(result.Nodes); strings.Contains(html, `class="hljs-comment"`) {
		t.Fatalf("a mid-line `#` was read as a line-leading comment:\n%s", html)
	}
	result, err = Highlight("#include <foo.h>\n", "armasm")
	if err != nil {
		t.Fatalf("highlight: %v", err)
	}
	if html := RenderHTML(result.Nodes); !strings.Contains(html, `class="hljs-comment"`) {
		t.Fatalf("a line-leading `#` must still be a comment:\n%s", html)
	}
}

// TestBeginKeywordsPrefersTheLongestMatch guards the beginKeywords compiler:
// JavaScript's `(?=\b|\s)` lookahead backtracks to a longer alternative when
// the boundary after a shorter prefix fails, so `defn` must not be read as
// `def`. RE2's leftmost-first alternation needs the longest word first.
func TestBeginKeywordsPrefersTheLongestMatch(t *testing.T) {
	result, err := Highlight("(defn f []) (def x 1)", "clojure")
	if err != nil {
		t.Fatalf("highlight: %v", err)
	}
	html := RenderHTML(result.Nodes)
	if !strings.Contains(html, "<span class=\"hljs-keyword\">defn</span>") {
		t.Fatalf("`defn` was not read as one keyword:\n%s", html)
	}
	if !strings.Contains(html, "<span class=\"hljs-keyword\">def</span>") {
		t.Fatalf("`def` was not read as a keyword:\n%s", html)
	}
}

// TestNonASCIIRuleStartIsNotSkipped guards the byte pre-filter: it is a set of
// bytes, and strings.IndexAny would decode a non-ASCII leading byte as an
// invalid rune and skip it. 1C's Cyrillic `процедура` rule must still match.
func TestNonASCIIRuleStartIsNotSkipped(t *testing.T) {
	result, err := Highlight("процедура Тест()", "1c")
	if err != nil {
		t.Fatalf("highlight: %v", err)
	}
	if html := RenderHTML(result.Nodes); !strings.Contains(html, `class="hljs-function"`) {
		t.Fatalf("a non-ASCII rule begin was skipped:\n%s", html)
	}
}
