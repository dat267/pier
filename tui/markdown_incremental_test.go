package tui

import (
	"reflect"
	"strings"
	"testing"
)

// The renderer keeps lexed blocks and rendered lines across updates, keyed by
// the source, the width and the link definitions. A reuse that does not hold
// shows up as a frame that differs from a cold render of the same text, which
// is what streaming deltas, width changes and appended definitions exercise.
func TestIncrementalRenderMatchesAColdRender(t *testing.T) {
	documents := []string{
		"# Title\n\nA [link][ref] and `code`.\n\n[ref]: https://example.com/1\n",
		"para one\n\n```go\ncode\n```\n\n- a\n- b\n",
		"| a | b |\n| - | - |\n| 1 | 2 |\n",
		"[ref]: https://example.com/2\n\nLate definition then [ref].\n",
		"## 漢字 heading\n\n漢字テキスト with 👨‍👩‍👧.\n\n> quote\n",
		"text\n\n```\nunclosed fence\n",
	}
	for _, document := range documents {
		label := strings.SplitN(document, "\n", 2)[0]
		for _, width := range []int{20, 40, 80} {
			streamed := NewMarkdown("", 0, 0, MarkdownTheme{}, nil, MarkdownOptions{})
			for cut := 1; cut <= len(document); cut += 7 {
				streamed.SetText(document[:cut])
				streamed.Render(width)
			}
			streamed.SetText(document)
			if got, want := streamed.Render(width), NewMarkdown(document, 0, 0, MarkdownTheme{}, nil, MarkdownOptions{}).Render(width); !reflect.DeepEqual(got, want) {
				t.Fatalf("%q at width %d: streamed render differs from a cold render", label, width)
			}
			// A width change and a later append must not reuse the previous frame.
			grown := document + "\nappended after a width change\n"
			streamed.Render(120)
			streamed.SetText(grown)
			if got, want := streamed.Render(width), NewMarkdown(grown, 0, 0, MarkdownTheme{}, nil, MarkdownOptions{}).Render(width); !reflect.DeepEqual(got, want) {
				t.Fatalf("%q at width %d after a width change: render differs from a cold render", label, width)
			}
		}
	}
}
