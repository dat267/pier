package highlight

import "testing"

// pi v1.4.0 (4a080252) autohotkey.js title variants use `:(?!=)` and `::(?!=)`.
// JavaScript backtracks to an earlier colon when the last is followed by "=",
// which the manual begins reproduce.
func TestAutoHotkeyTitleBacktracking(t *testing.T) {
	for _, tc := range []struct{ name, code, html string }{
		{"double then equals", "a::=b", `<span class="hljs-title">a:</span>:=b`},
		{"double colon", "a::b", `<span class="hljs-title">a::</span>b`},
		{"assignment", "a:=b", "a:=b"},
		{"single colon", "a:b", `<span class="hljs-title">a:</span>b`},
		{"next line assignment", "x::=1\ny:=2\n",
			`<span class="hljs-title">x:</span>:=<span class="hljs-number">1</span>` + "\ny:=" + `<span class="hljs-number">2</span>` + "\n"},
		{"random differential case", "^!s::*\n-(** 0^!s::=+(b\n",
			`<span class="hljs-title">^!s::</span>*` + "\n" + `<span class="hljs-title">-(** 0^!s:</span>:=+(b` + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Highlight(tc.code, "autohotkey")
			if err != nil {
				t.Fatal(err)
			}
			if got := RenderHTML(result.Nodes); got != tc.html {
				t.Fatalf("HTML = %q; want %q", got, tc.html)
			}
		})
	}
}
