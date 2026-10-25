package markdown

import (
	"reflect"
	"testing"
)

// Marked's inlineText emits an unmatched tilde run as one text token. It does
// not retry strict strikethrough in the middle of that run.
func TestUnmatchedTildeRunIsNotRetried(t *testing.T) {
	for _, input := range []string{"~~~x~~", "~~~~x~~", "a~~~~x~~", "~~x~~~"} {
		if got, want := ParseInline(input), []Inline{{Kind: InlineText, Text: input}}; !reflect.DeepEqual(got, want) {
			t.Errorf("ParseInline(%q) = %+v, want literal text", input, got)
		}
	}
}

// pi v1.4.0 markdown.ts StrictStrikethroughTokenizer uses escaped-character
// atoms and JavaScript whitespace when recognizing a two-tilde pair.
func TestStrictStrikethroughUsesEscapeAtoms(t *testing.T) {
	for _, item := range []struct {
		input string
		want  []Inline
	}{
		{`~~x\~~`, []Inline{{Kind: InlineText, Text: "~~x"}, {Kind: InlineEscape, Raw: `\~`, Text: "~"}, {Kind: InlineText, Text: "~"}}},
		{`~~x\ ~~`, []Inline{{Kind: InlineStrikethrough, Text: `x\ `, Children: []Inline{{Kind: InlineText, Text: `x\ `}}}}},
		{"~~\u0085x~~", []Inline{{Kind: InlineStrikethrough, Text: "\u0085x", Children: []Inline{{Kind: InlineText, Text: "\u0085x"}}}}},
		{"~~\ufeffx~~", []Inline{{Kind: InlineText, Text: "~~\ufeffx~~"}}},
		{"~~x\\\n~~", []Inline{{Kind: InlineText, Text: "~~x"}, {Kind: InlineHardBreak}, {Kind: InlineText, Text: "~~"}}},
	} {
		if got := ParseInline(item.input); !reflect.DeepEqual(got, item.want) {
			t.Errorf("ParseInline(%q) = %+v, want %+v", item.input, got, item.want)
		}
	}
}
