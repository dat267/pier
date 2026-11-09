package highlight

import "testing"

// pi v1.5.0 (f9a26365) packages/coding-agent/src/utils/syntax-highlight.ts
// renders highlight.js 10.7.3's HTML. Two grammars give an empty parameter list
// its own variant with no class at all: python's `PARAMS` starts with
// `{ className: "", begin: /\(\s*\)/, skip: true }` (languages/python.js, "Exclude
// params in functions without params") and the arrow-function variant list in
// languages/javascript.js with `{ className: null, begin: /\(\s*\)/, skip: true }`.
// `inherit()` copies the variant's own keys over the wrapper's, so a class the
// variant declares empty must stay empty: upstream renders `def f():` with no
// `hljs-params` span. The port could not tell "declared empty" from "not
// declared" and fell back to the wrapper's class, emitting an empty span.
func TestEmptyParameterListHasNoParamsSpan(t *testing.T) {
	for _, test := range []struct {
		language string
		code     string
		want     string
	}{
		{
			language: "python",
			code:     "def f():",
			want:     `<span class="hljs-function"><span class="hljs-keyword">def</span> <span class="hljs-title">f</span>():</span>`,
		},
		{
			language: "javascript",
			code:     "const a = () => 1;",
			want:     `<span class="hljs-keyword">const</span> a = <span class="hljs-function">() =&gt;</span> <span class="hljs-number">1</span>;`,
		},
	} {
		result, err := Highlight(test.code, test.language)
		if err != nil {
			t.Fatalf("%s: %v", test.language, err)
		}
		if got := RenderHTML(result.Nodes); got != test.want {
			t.Errorf("%s %q:\n got %s\nwant %s", test.language, test.code, got, test.want)
		}
	}
}

// A non-empty parameter list keeps its span, so the fix is not a blanket
// removal.
func TestNonEmptyParameterListKeepsParamsSpan(t *testing.T) {
	for _, test := range []struct {
		language string
		code     string
		want     string
	}{
		{
			language: "python",
			code:     "def f(a):",
			want:     `<span class="hljs-function"><span class="hljs-keyword">def</span> <span class="hljs-title">f</span>(<span class="hljs-params">a</span>):</span>`,
		},
		{
			language: "javascript",
			code:     "const a = (x) => 1;",
			want:     `<span class="hljs-keyword">const</span> a = <span class="hljs-function">(<span class="hljs-params">x</span>) =&gt;</span> <span class="hljs-number">1</span>;`,
		},
	} {
		result, err := Highlight(test.code, test.language)
		if err != nil {
			t.Fatalf("%s: %v", test.language, err)
		}
		if got := RenderHTML(result.Nodes); got != test.want {
			t.Errorf("%s %q:\n got %s\nwant %s", test.language, test.code, got, test.want)
		}
	}
}
