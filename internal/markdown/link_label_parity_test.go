package markdown

import (
	"reflect"
	"testing"
)

// Marked's outputLink rejects an outer hyperlink when parsing its label emits
// another explicit hyperlink. An image in the label is still allowed.
func TestLinkLabelsDoNotNestHyperlinks(t *testing.T) {
	inner := Inline{Kind: InlineLink, Text: "x", Dest: "inner", Children: []Inline{{Kind: InlineText, Text: "x"}}}
	want := []Inline{{Kind: InlineText, Text: "["}, inner, {Kind: InlineText, Text: "](outer)"}}
	if got := ParseInline("[[x](inner)](outer)"); !reflect.DeepEqual(got, want) {
		t.Errorf("nested hyperlinks = %+v, want %+v", got, want)
	}
	image := Inline{Kind: InlineImage, Text: "x", Dest: "image", Children: []Inline{{Kind: InlineText, Text: "x"}}}
	want = []Inline{{Kind: InlineLink, Text: "![x](image)", Dest: "outer", Children: []Inline{image}}}
	if got := ParseInline("[![x](image)](outer)"); !reflect.DeepEqual(got, want) {
		t.Errorf("linked image = %+v, want %+v", got, want)
	}
}

// pi v1.4.0 markdown.ts uses Marked 18's outputLink, which unescapes square
// brackets in labels before recursively parsing their inline content.
func TestLinkLabelsUnescapeSquareBrackets(t *testing.T) {
	for _, item := range []struct {
		input string
		kind  InlineKind
	}{
		{`[a\[b\]](url)`, InlineLink},
		{`![a\[b\]](url)`, InlineImage},
	} {
		want := []Inline{{Kind: item.kind, Text: "a[b]", Dest: "url", Children: []Inline{{Kind: InlineText, Text: "a[b]"}}}}
		if got := ParseInline(item.input); !reflect.DeepEqual(got, want) {
			t.Errorf("ParseInline(%q) = %+v, want %+v", item.input, got, want)
		}
	}
}
