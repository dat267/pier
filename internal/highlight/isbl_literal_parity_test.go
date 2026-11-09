package highlight

import "testing"

// pi v1.5.0 (f9a26365) renders highlight.js 10.7.3, whose isbl grammar builds
// one keyword object `{ $pattern, keyword, built_in, class, literal }` and
// reuses it in every mode (languages/isbl.js). `compileKeywords` fills one map
// per mode in insertion order, so a word listed twice ends up in the class
// applied last: `true`, `false`, `null` and `nil` appear in the 1C constant run
// as `TRUE`, `FALSE`, `NULL` and `nil`, and in `literal` as themselves, and
// `literal` comes after `built_in` — so the reference reports `literal`. The
// port applies its own class order (built_in last, which TypeScript's `void`
// needs), so it reported `built_in` for those four.
func TestIsblConstantCollisionsAreLiterals(t *testing.T) {
	for _, word := range []string{"true", "false", "null", "nil"} {
		result, err := Highlight(word, "isbl")
		if err != nil {
			t.Fatalf("%q: %v", word, err)
		}
		want := `<span class="hljs-variable"><span class="hljs-literal">` + word + `</span></span>`
		if got := RenderHTML(result.Nodes); got != want {
			t.Errorf("%q:\n got %s\nwant %s", word, got, want)
		}
	}
}

// A word that the constant run lists and `literal` does not keeps its built_in
// class, so the fix did not empty the list.
func TestIsblBuiltInWordsKeepTheirClass(t *testing.T) {
	for _, word := range []string{"YES_VALUE", "NO_VALUE"} {
		result, err := Highlight(word, "isbl")
		if err != nil {
			t.Fatalf("%q: %v", word, err)
		}
		want := `<span class="hljs-variable"><span class="hljs-built_in">` + word + `</span></span>`
		if got := RenderHTML(result.Nodes); got != want {
			t.Errorf("%q:\n got %s\nwant %s", word, got, want)
		}
	}
}
