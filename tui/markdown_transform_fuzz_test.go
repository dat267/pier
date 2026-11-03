package tui

import (
	"strings"
	"testing"
)

// FuzzMarkdownTransformIncrementalMatchesFull exercises the transformed
// streaming cache (D356): every prefix rendered incrementally must equal a
// fresh component rendering the same prefix and transform.
func FuzzMarkdownTransformIncrementalMatchesFull(f *testing.F) {
	for _, seed := range []string{
		incrementalSample,
		"# h\n\n**bold** and `code`\n\n- a\n- b\n",
		"```go\nfunc main() {}\n```\n",
		"",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 512 {
			return
		}
		transform := func(s string, _ int) string { return s }
		switch len(source) % 4 {
		case 1:
			transform = func(s string, _ int) string { return "> " + s }
		case 2:
			transform = func(s string, _ int) string { return strings.Replace(s, "a", "A", 1) }
		case 3:
			transform = func(s string, _ int) string {
				index := strings.LastIndexByte(s, '\n')
				if index < 0 {
					return strings.ToUpper(s)
				}
				return s[:index+1] + strings.ToUpper(s[index+1:])
			}
		}
		streamed := NewMarkdown("", 1, 0, mdTestTheme(), nil, MarkdownOptions{Transform: transform})
		for n := 0; n <= len(source); n++ {
			streamed.SetText(source[:n])
			got := streamed.Render(40)
			fresh := NewMarkdown(source[:n], 1, 0, mdTestTheme(), nil, MarkdownOptions{Transform: transform})
			want := fresh.Render(40)
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("prefix %d/%d:\n got %q\nwant %q", n, len(source), got, want)
			}
		}
	})
}
