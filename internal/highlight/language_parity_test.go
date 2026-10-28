package highlight

import "testing"

// pi v1.4.0 (4a080252) highlight.js language reference behavior for cases the
// generated language samples and earlier corpora did not reach.
func TestLanguageParityRegressions(t *testing.T) {
	for _, tc := range []struct{ name, language, code, html string }{
		// swift.js PLAIN_KEYWORDS appends "_|0".
		{"swift underscore", "swift", "_", `<span class="hljs-keyword">_</span>`},
		// crmsh.js has no facet/instance-of/name-brace modes.
		{"crmsh property", "crmsh", "property x[a{\n", `<span class="hljs-keyword">property</span><span class="hljs-title"> </span>x[a{` + "\n"},
		{"crmsh attributes", "crmsh", "property stonith-enabled=false*[a{\n",
			`<span class="hljs-keyword">property</span><span class="hljs-title"> </span><span class="hljs-attr">stonith-enabled=</span><span class="hljs-literal">false</span>*[a{` + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Highlight(tc.code, tc.language)
			if err != nil {
				t.Fatal(err)
			}
			if got := RenderHTML(result.Nodes); got != tc.html {
				t.Fatalf("HTML = %q; want %q", got, tc.html)
			}
		})
	}
}

// The reference's keyword guard and JSDoc variable set relevance 0; number
// variants inherit it from the wrapper mode.
func TestJavaScriptRelevanceMatchesUpstream(t *testing.T) {
	for _, tc := range []struct {
		code string
		want int
	}{
		{"$ca", 0}, {"var", 1}, {"var x = 1;", 1}, {"1;", 0}, {"for", 0},
	} {
		result, err := Highlight(tc.code, "javascript")
		if err != nil {
			t.Fatal(err)
		}
		if result.Relevance != tc.want {
			t.Errorf("%q relevance = %d; want %d", tc.code, result.Relevance, tc.want)
		}
	}
}

// highlightAuto highlights candidates with ignoreIllegals false. R's illegal
// `->` therefore yields plain text and relevance 0, not a scored candidate.
func TestHighlightAutoDoesNotIgnoreIllegals(t *testing.T) {
	result, name := highlightAuto("1 ->", []string{"r"})
	if name != "" || result.Relevance != 0 {
		t.Fatalf("auto result name=%q relevance=%d; want plaintext", name, result.Relevance)
	}
	if html := RenderHTML(result.Nodes); html != "1 -&gt;" {
		t.Fatalf("HTML = %q; want plain text", html)
	}
}
