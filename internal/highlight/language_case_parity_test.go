package highlight

import "testing"

// pi v1.4.0 utils/syntax-highlight.ts delegates supportsLanguage and highlight
// to highlight.js 10.7.3. Its getLanguage lowercases names with JS semantics.
func TestLanguageLookupUsesJSCaseFolding(t *testing.T) {
	for _, item := range []struct {
		name string
		want bool
	}{
		{"Go", true}, {"GO", true}, {"Js", true}, {"TYPESCRIPT", true},
		{"AWK", true}, {"İni", false},
	} {
		if got := SupportsLanguage(item.name); got != item.want {
			t.Errorf("SupportsLanguage(%q) = %v, want %v", item.name, got, item.want)
		}
	}
	result, err := Highlight("package main", "Go")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := RenderHTML(result.Nodes), `<span class="hljs-keyword">package</span> main`; got != want {
		t.Fatalf("mixed-case Go highlight = %q, want %q", got, want)
	}
}
