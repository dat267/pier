package markdown

import (
	"reflect"
	"testing"
)

// pi v1.4.0 markdown.ts tokenizeInlineLatex accepts inline double-dollar math
// as well as single dollars, and applies JS whitespace and newline rules.
func TestInlineDollarDelimitersMatchUpstream(t *testing.T) {
	for _, item := range []struct {
		input string
		want  []Inline
	}{
		{"a $$x$$ b", []Inline{{Kind: InlineText, Text: "a "}, {Kind: InlineLatex, Raw: "$$x$$", Text: "x"}, {Kind: InlineText, Text: " b"}}},
		{"$$ x $$", []Inline{{Kind: InlineLatex, Raw: "$$ x $$", Text: " x "}}},
		{"$$x+1", []Inline{{Kind: InlineLatex, Raw: "$$x+1", Text: "x+1", Pending: true}}},
		{"$x\ny$", []Inline{{Kind: InlineText, Text: "$x\ny$"}}},
		{"$x+\ny$", []Inline{{Kind: InlineText, Text: "$x+\ny$"}}},
		{"$\u0085x$", []Inline{{Kind: InlineLatex, Raw: "$\u0085x$", Text: "\u0085x"}}},
		{"$\ufeffx$", []Inline{{Kind: InlineText, Text: "$\ufeffx$"}}},
		{"$x\u0085$", []Inline{{Kind: InlineLatex, Raw: "$x\u0085$", Text: "x\u0085"}}},
		{"$x\ufeff$", []Inline{{Kind: InlineText, Text: "$x\ufeff$"}}},
	} {
		if got := ParseInline(item.input); !reflect.DeepEqual(got, item.want) {
			t.Errorf("ParseInline(%q) = %+v, want %+v", item.input, got, item.want)
		}
	}
}

// pi v1.4.0 packages/tui/src/components/markdown.ts tokenizeInlineLatex and
// findClosingDelimiter handle both backslash delimiter pairs, reject empty or
// multiline completed spans, and skip escaped closing delimiters.
func TestInlineBackslashLatexMatchesUpstream(t *testing.T) {
	for _, item := range []struct {
		input string
		want  []Inline
	}{
		{`a \[x+1\] b`, []Inline{{Kind: InlineText, Text: "a "}, {Kind: InlineLatex, Raw: `\[x+1\]`, Text: "x+1"}, {Kind: InlineText, Text: " b"}}},
		{`\[x`, []Inline{{Kind: InlineLatex, Raw: `\[x`, Text: "x", Pending: true}}},
		{`\[\]`, []Inline{{Kind: InlineEscape, Raw: `\[`, Text: "["}, {Kind: InlineEscape, Raw: `\]`, Text: "]"}}},
		{`\(\)`, []Inline{{Kind: InlineEscape, Raw: `\(`, Text: "("}, {Kind: InlineEscape, Raw: `\)`, Text: ")"}}},
		{`\(a\\)b\)`, []Inline{{Kind: InlineLatex, Raw: `\(a\\)b\)`, Text: `a\\)b`}}},
		{"\\(a\nb\\)", []Inline{{Kind: InlineEscape, Raw: `\(`, Text: "("}, {Kind: InlineText, Text: "a\nb"}, {Kind: InlineEscape, Raw: `\)`, Text: ")"}}},
	} {
		if got := ParseInline(item.input); !reflect.DeepEqual(got, item.want) {
			t.Errorf("ParseInline(%q) = %+v, want %+v", item.input, got, item.want)
		}
	}
}
