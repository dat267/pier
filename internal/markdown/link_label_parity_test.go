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

// TestLinkLabelUsesFullUnicodeCaseMapping pins normalizeLinkLabel against the
// reference's `toLowerCase().replace(/\s+/g, ' ')`. JavaScript lowercases with
// full Unicode case mapping, which expands U+0130 "İ" to "i" plus a combining
// dot (two code points); Go's unicode.ToLower applies the simple mapping and
// yields a single "i". With the simple mapping `[x][i]` resolved against a
// `[İ]:` definition here, and the reference leaves it as text. Latin-1 and
// Greek match under both mappings, so only the expansion shows the difference.
func TestLinkLabelUsesFullUnicodeCaseMapping(t *testing.T) {
	if got := normalizeLinkLabel("İ"); got != "i\u0307" {
		t.Fatalf("normalizeLinkLabel(İ) = %q, want %q (full case mapping)", got, "i\u0307")
	}
	if got := normalizeLinkLabel("i"); got != "i" {
		t.Fatalf("normalizeLinkLabel(i) = %q, want %q", got, "i")
	}
	// Whitespace still collapses to one space, and is not trimmed, as the
	// reference's replace does.
	if got := normalizeLinkLabel("  A \t B  "); got != " a b " {
		t.Fatalf("normalizeLinkLabel whitespace = %q, want %q", got, " a b ")
	}
	// The resolution the mapping decides: a plain "i" reference must not match
	// an "İ" definition, while "İ" itself must.
	blocks, defs := collectDefinitions(Lex("[x][i]\n\n[İ]: /u"), nil)
	parseInlines(blocks, &inlineState{links: linksFor(defs)})
	for _, block := range blocks {
		for _, inline := range block.Inlines {
			if inline.Kind == InlineLink {
				t.Fatal("[x][i] resolved against an [İ]: definition")
			}
		}
	}
	blocks, defs = collectDefinitions(Lex("[x][İ]\n\n[İ]: /u"), nil)
	parseInlines(blocks, &inlineState{links: linksFor(defs)})
	linked := false
	for _, block := range blocks {
		for _, inline := range block.Inlines {
			if inline.Kind == InlineLink {
				linked = true
			}
		}
	}
	if !linked {
		t.Fatal("[x][İ] did not resolve against its own definition")
	}
}
