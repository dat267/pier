package highlight

import (
	"runtime"
	"strings"
	"testing"
)

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

// D536: the mode buffer grows by appending to bytes, not by `+=` on a string,
// which copied the whole buffer per lexeme. stata over an HTTP body (as HTTP's
// auto-detection runs it, pi v1.6.0, highlight.js 10.7.3) keeps one long
// buffer of many lexemes; doubling the input must about double the bytes
// allocated, where the copies made it four times. The buffer's text is
// checked through the HTML, which must keep the input whole.
func TestModeBufferGrowsLinearly(t *testing.T) {
	allocated := func(lines int) uint64 {
		code := strings.Repeat("Hello, world!\nHTTP/1.1 200 OK\nContent-Length: 13\n", lines)
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		result, err := HighlightWith(code, "stata", true)
		if err != nil {
			t.Fatal(err)
		}
		runtime.ReadMemStats(&after)
		if text := nodesText(result.Nodes); text != code {
			t.Fatalf("%d lines: the HTML lost text", lines)
		}
		return after.TotalAlloc - before.TotalAlloc
	}
	small, large := allocated(400), allocated(1600)
	if ratio := float64(large) / float64(small); ratio > 6 {
		t.Fatalf("4x the input allocated %.1fx the bytes (%d to %d), want about 4x", ratio, small, large)
	}
}

func nodesText(nodes []*Node) string {
	var b strings.Builder
	var walk func([]*Node)
	walk = func(nodes []*Node) {
		for _, n := range nodes {
			b.WriteString(n.Text)
			walk(n.Children)
		}
	}
	walk(nodes)
	return b.String()
}
