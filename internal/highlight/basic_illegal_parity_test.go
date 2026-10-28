package highlight

import "testing"

// pi v1.4.0 syntax-highlight.ts loads highlight.js 10.7.3 languages/basic.js.
// Its illegal string '^\.' evaluates to '^.' in JavaScript: the compiled regex
// rejects any line start not already consumed by a begin rule, not just dots.
func TestBASICIllegalPatternMatchesUpstream(t *testing.T) {
	for _, c := range []struct{ input, want string }{
		{"PRINT 1", "PRINT 1"},
		{" 10 PRINT 1", " 10 PRINT 1"},
		{"10 PRINT 1", `<span class="hljs-symbol">10 </span><span class="hljs-keyword">PRINT</span> <span class="hljs-number">1</span>`},
		{".abc", ".abc"},
	} {
		result, err := Highlight(c.input, "basic")
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); got != c.want {
			t.Errorf("BASIC %q = %q, want %q", c.input, got, c.want)
		}
	}
}
