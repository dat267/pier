package highlight

import "testing"

// pi v1.4.0 syntax-highlight.ts loads highlight.js 10.7.3 languages/stylus.js.
// LOOKAHEAD_TAG_END requires a following punctuation/JS-whitespace character;
// EOF is not a match. Literal expectations were checked against that engine.
func TestStylusSelectorLookaheadMatchesUpstream(t *testing.T) {
	for _, c := range []struct{ input, want string }{
		{".card", ".card"},
		{"#main", "#main"},
		{"a", "a"},
		{"&:hover", "&amp;:hover"},
		{".card ", `<span class="hljs-selector-class">.card</span> `},
		{".card,", `<span class="hljs-selector-class">.card</span>,`},
		{".card\n", `<span class="hljs-selector-class">.card</span>` + "\n"},
		{".card\u00A0", `<span class="hljs-selector-class">.card</span>` + "\u00A0"},
		{".card\u2028", `<span class="hljs-selector-class">.card</span>` + "\u2028"},
		{".card\uFEFF", `<span class="hljs-selector-class">.card</span>` + "\uFEFF"},
		{".card\u0085", ".card\u0085"},
	} {
		result, err := Highlight(c.input, "stylus")
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); got != c.want {
			t.Errorf("Stylus %q = %q, want %q", c.input, got, c.want)
		}
	}
}
