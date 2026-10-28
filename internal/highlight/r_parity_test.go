package highlight

import "testing"

// pi v1.4.0 (4a080252) r.js implements `beforeMatch` through a compiler
// extension: the preceding non-identifier character only starts the number
// mode when a number variant follows. Otherwise the root illegal `->` aborts.
func TestRNumberAndIllegalParity(t *testing.T) {
	for _, tc := range []struct {
		code, html string
		illegal    bool
	}{
		{"->", "-&gt;", true},
		{"[->", "[-&gt;", true},
		{"'x' ->", "&#x27;x&#x27; -&gt;", true},
		{"1", "1", false},
		{" 1", ` <span class="hljs-number">1</span>`, false},
		{"x * 2", `x * <span class="hljs-number">2</span>`, false},
		{"x.1", "x.1", false},
		{"x <- 1", "x &lt;- 1", false},
	} {
		result, err := Highlight(tc.code, "r")
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); got != tc.html || result.Illegal != tc.illegal {
			t.Errorf("%q: html=%q illegal=%v; want %q %v", tc.code, got, result.Illegal, tc.html, tc.illegal)
		}
	}
}
