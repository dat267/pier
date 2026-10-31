package markdown

import (
	"reflect"
	"testing"
)

// pi 4a080252 packages/tui/src/components/markdown.ts delegates links to
// Marked 18.0.11. Balanced URL parentheses belong to the destination, while
// parentheses inside angle destinations and quoted titles do not close links.
func TestInlineLinkParenthesesMatchUpstream(t *testing.T) {
	for _, item := range []struct {
		input string
		dest  string
		title string
	}{
		{"[doc](https://example.invalid/a(b)c)", "https://example.invalid/a(b)c", ""},
		{"[doc](https://example.invalid/a(b(c)d)e)", "https://example.invalid/a(b(c)d)e", ""},
		{"[doc](<https://example.invalid/a(b>)", "https://example.invalid/a(b", ""},
		{"[doc](https://example.invalid/a \"title (open\")", "https://example.invalid/a", "title (open"},
		{"[doc](https://example.invalid/a(b)c \"title ) close\")", "https://example.invalid/a(b)c", "title ) close"},
		{"[doc](https://example.invalid/plain)", "https://example.invalid/plain", ""},
	} {
		want := []Inline{{Kind: InlineLink, Text: "doc", Dest: item.dest, Title: item.title, Children: []Inline{{Kind: InlineText, Text: "doc"}}}}
		if got := ParseInline(item.input); !reflect.DeepEqual(got, want) {
			t.Errorf("ParseInline(%q) = %+v, want %+v", item.input, got, want)
		}
	}
}
