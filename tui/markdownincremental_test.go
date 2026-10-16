package tui

import (
	"runtime"
	"strings"
	"testing"
)

const incrementalSample = `# Title

Intro paragraph with **bold**, ` + "`code`" + ` and a [link](https://example.com).

- item one
- item two

> a quote
> second line

` + "```go" + `
func main() {
	fmt.Println("hi")
}
` + "```" + `

| a | b |
| - | - |
| 1 | 2 |

Final paragraph that keeps growing.`

// TestMarkdownIncrementalMatchesFullRender feeds the sample one byte at a time
// (as streaming does) and requires the incrementally-rendered output to be
// byte-identical to a fresh render of every prefix.
func TestMarkdownIncrementalMatchesFullRender(t *testing.T) {
	theme := mdTestTheme()
	for _, width := range []int{20, 80} {
		incremental := NewMarkdown("", 1, 0, theme, nil, MarkdownOptions{})
		for n := 1; n <= len(incrementalSample); n++ {
			incremental.SetText(incrementalSample[:n])
			got := incremental.Render(width)

			fresh := NewMarkdown(incrementalSample[:n], 1, 0, theme, nil, MarkdownOptions{})
			want := fresh.Render(width)

			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("width %d prefix %d:\n got %q\nwant %q", width, n, got, want)
			}
		}
	}
}

// TestMarkdownStreamingReusesStablePrefix pins that appending to a rendered
// message only re-renders the changed tail, not every token.
func TestMarkdownStreamingReusesStablePrefix(t *testing.T) {
	theme := mdTestTheme()
	m := NewMarkdown(incrementalSample, 0, 0, theme, nil, MarkdownOptions{})
	m.Render(80)
	full := m.renderedTokens
	if full < 5 {
		t.Fatalf("first render rendered %d tokens, want the whole sample", full)
	}

	// The reuse cache must not depend on GC timing: force a collection between
	// the two renders (a live transcript lets the GC run at any frame).
	runtime.GC()
	runtime.GC()
	m.SetText(incrementalSample + " one more word")
	m.Render(80)
	if tail := m.renderedTokens - full; tail > 3 {
		t.Fatalf("append re-rendered %d tokens, want only the changed tail", tail)
	}
}

// TestMarkdownStreamingIncrementalLexRendersTail streams a long message in
// chunks and requires the total block renders to stay near one full render:
// both the lex (markdown.LexFrom) and the line cache must carry the stable
// prefix forward, so a four-chunk stream costs about one render plus the new
// blocks rather than one full render per chunk.
func TestMarkdownStreamingIncrementalLexRendersTail(t *testing.T) {
	theme := mdTestTheme()
	var builder strings.Builder
	for i := 0; i < 400; i++ {
		builder.WriteString("Paragraph with **bold** and `code` and a [link](https://example.com).\n\n")
	}
	message := builder.String()

	streamed := NewMarkdown("", 0, 0, theme, nil, MarkdownOptions{})
	for chunk := 1; chunk <= 4; chunk++ {
		streamed.SetText(message[:len(message)*chunk/4])
		streamed.Render(80)
	}

	fresh := NewMarkdown(message, 0, 0, theme, nil, MarkdownOptions{})
	fresh.Render(80)
	if streamed.renderedTokens > fresh.renderedTokens+20 {
		t.Fatalf("streaming rendered %d blocks, want about one full render (%d)",
			streamed.renderedTokens, fresh.renderedTokens)
	}
}

// TestMarkdownStreamingDoesNotDriftFromFreshRender checks the exact output of a
// chunked stream against a single render of the final source.
func TestMarkdownStreamingDoesNotDriftFromFreshRender(t *testing.T) {
	theme := mdTestTheme()
	streamed := NewMarkdown("", 0, 0, theme, nil, MarkdownOptions{})
	for n := 1; n <= len(incrementalSample); n++ {
		streamed.SetText(incrementalSample[:n])
		if n%17 != 0 {
			continue
		}
		got := streamed.Render(80)
		fresh := NewMarkdown(incrementalSample[:n], 0, 0, theme, nil, MarkdownOptions{})
		want := fresh.Render(80)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("prefix %d: streamed output drifted from a fresh render", n)
		}
	}
}
