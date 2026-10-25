package highlight

import "testing"

// pi v1.4.0 utils/syntax-highlight.ts uses highlight.js 10.7.3 languages/yaml.js.
// Literal HTML expectations were read from that pinned implementation. Its
// three whitespace rules use JS \s and \S, not RE2's ASCII-only classes.
func TestYAMLUsesJavaScriptWhitespace(t *testing.T) {
	for _, item := range []struct{ input, want string }{
		{"foo\u2003bar", "<span class=\"hljs-string\">foo</span>\u2003<span class=\"hljs-string\">bar</span>"},
		{"[foo\u2003bar]", "[<span class=\"hljs-string\">foo</span>\u2003<span class=\"hljs-string\">bar</span>]"},
		{"---\u2003", "<span class=\"hljs-meta\">---\u2003</span>"},
		{"foo\u0085bar", "<span class=\"hljs-string\">foo\u0085bar</span>"},
		{"---\u0085", "<span class=\"hljs-string\">---\u0085</span>"},
	} {
		result, err := Highlight(item.input, "yaml")
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); got != item.want {
			t.Errorf("YAML %q = %q, want %q", item.input, got, item.want)
		}
	}
}
