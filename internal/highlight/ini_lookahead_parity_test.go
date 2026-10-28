package highlight

import "testing"

// pi v1.4.0 syntax-highlight.ts uses highlight.js 10.7.3 languages/ini.js.
// DOTTED_KEY's lookahead is part of the match, so an unmatched bare key is
// still subject to the root illegal rule. Expectations are from that engine.
func TestINIKeyLookaheadPreservesIllegalFallback(t *testing.T) {
	for _, c := range []struct{ input, want string }{
		{"x = 1\nbare", "x = 1\nbare"},
		{"bare", "bare"},
		{"owner.name", "owner.name"},
		{"x = 1\nbare = 2", `<span class="hljs-attr">x</span> = <span class="hljs-number">1</span>` + "\n" + `<span class="hljs-attr">bare</span> = <span class="hljs-number">2</span>`},
		{`owner.name = "Tom"`, `<span class="hljs-attr">owner.name</span> = <span class="hljs-string">&quot;Tom&quot;</span>`},
	} {
		result, err := Highlight(c.input, "ini")
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); got != c.want {
			t.Errorf("INI %q = %q, want %q", c.input, got, c.want)
		}
	}
}
