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
