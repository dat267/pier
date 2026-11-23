package highlight

import "testing"

// A zero-width match at the end that advances past it ends the loop, as the
// reference's next exec (pi v1.6.0, highlight.js 10.7.3 src/highlight.js
// _highlight, lastIndex past the input) finds nothing. Matching again at the
// clamped end spun until the iteration cap, a million empty matches (140 ms)
// for a lone LaTeX backslash; the HTML was read off the pinned highlight.js.
func TestZeroWidthMatchPastTheEndEndsTheLoop(t *testing.T) {
	for code, want := range map[string]string{
		`\`:   `<span class="hljs-keyword">\</span>`,
		`a\`:  `a<span class="hljs-keyword">\</span>`,
		`x \`: `x <span class="hljs-keyword">\</span>`,
	} {
		var got string
		allocs := testing.AllocsPerRun(1, func() {
			result, err := HighlightWith(code, "latex", true)
			if err != nil {
				t.Fatal(err)
			}
			got = RenderHTML(result.Nodes)
		})
		if got != want {
			t.Errorf("latex %q = %q, want %q", code, got, want)
		}
		if allocs > 1000 {
			t.Errorf("latex %q took %.0f allocations, want the loop to end at the input's end", code, allocs)
		}
	}
}
