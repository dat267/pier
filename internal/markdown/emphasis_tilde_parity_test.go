package markdown

import (
	"reflect"
	"testing"
)

// GFM's left-delimiter rule excludes tilde from punctuation for underscores
// too, so a preceding letter still disallows the intraword opener.
func TestUnderscoreOpenersTreatTildeAsWord(t *testing.T) {
	for _, input := range []string{"中文__~~x__", "a_~~x_"} {
		if got, want := ParseInline(input), []Inline{{Kind: InlineText, Text: input}}; !reflect.DeepEqual(got, want) {
			t.Errorf("ParseInline(%q) = %+v, want literal text", input, got)
		}
	}
}

// pi v1.4.0 markdown.ts with marked-cjk-friendly 0.1.2 uses the GFM asterisk
// delimiter rules, where tilde counts as word content, not punctuation.
func TestAsteriskDelimitersTreatTildeAsWord(t *testing.T) {
	for _, item := range []struct {
		input string
		want  []Inline
	}{
		{"*~~*a", []Inline{{Kind: InlineEmphasis, Text: "~~", Children: []Inline{{Kind: InlineText, Text: "~~"}}}, {Kind: InlineText, Text: "a"}}},
		{"**😀**~~", []Inline{{Kind: InlineText, Text: "**😀**~~"}}},
		{"*x~~**`foo", []Inline{{Kind: InlineEmphasis, Text: "x~~", Children: []Inline{{Kind: InlineText, Text: "x~~"}}}, {Kind: InlineText, Text: "*`foo"}}},
	} {
		if got := ParseInline(item.input); !reflect.DeepEqual(got, item.want) {
			t.Errorf("ParseInline(%q) = %+v, want %+v", item.input, got, item.want)
		}
	}
}
