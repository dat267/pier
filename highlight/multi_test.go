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
