package highlight

import "testing"

// pi v1.4.0 utils/syntax-highlight.ts loads highlight.js 10.7.3, whose
// languages/json.js uses JavaScript /\S/ for illegal text. Expectations were
// checked against that pinned implementation, including non-whitespace controls.
func TestJSONUsesJavaScriptWhitespace(t *testing.T) {
	for _, space := range []string{" ", "\n", "\v", "\u00A0", "\u2028", "\u2029", "\uFEFF"} {
		for _, c := range []struct{ input, want string }{
			{space + `{"n":1}`, space + `{<span class="hljs-attr">&quot;n&quot;</span>:<span class="hljs-number">1</span>}`},
			{"{" + space + `"n":1}`, "{" + space + `<span class="hljs-attr">&quot;n&quot;</span>:<span class="hljs-number">1</span>}`},
			{"[" + space + "1]", "[" + space + `<span class="hljs-number">1</span>]`},
		} {
			result, err := Highlight(c.input, "json")
			if err != nil {
				t.Fatal(err)
			}
			if got := RenderHTML(result.Nodes); got != c.want {
				t.Errorf("JSON %q = %q, want %q", c.input, got, c.want)
			}
		}
	}
	for _, nonSpace := range []string{"\u0085", "\u200B"} {
		input := nonSpace + `{"n":1}`
		result, err := Highlight(input, "json")
		if err != nil {
			t.Fatal(err)
		}
		want := nonSpace + `{&quot;n&quot;:1}`
		if got := RenderHTML(result.Nodes); got != want {
			t.Errorf("JSON %q = %q, want %q", input, got, want)
		}
	}
}
