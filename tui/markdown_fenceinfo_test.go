package tui

import "testing"

// A fenced code block's info string is unescaped, not shown verbatim: pi
// v1.5.0 (f9a26365) packages/tui/src/components/markdown.ts renders
// “ ```${token.lang}“, and marked's fences tokenizer produces
// `lang: text.trim().replace(anyPunctuation, "$1")`. A backslash-escaped
// punctuation byte therefore reaches the fence header and the highlighter
// without its backslash (A441).
func TestMarkdownFenceInfoUnescapesPunctuation(t *testing.T) {
	theme := mdTestTheme()
	got := mdTrimmedLines(NewMarkdown("```js\\/ts\nx\n```", 0, 0, theme, nil, MarkdownOptions{}), 20)
	want := []string{"<cbb>```js/ts</cbb>", "  <cb>x</cb>", "<cbb>```</cbb>"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i, got[i], want[i])
		}
	}
}
