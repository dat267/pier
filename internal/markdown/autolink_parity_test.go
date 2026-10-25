package markdown

import (
	"reflect"
	"testing"
)

// link builds the inline a Marked gfm `url` token produces: the raw spelling as
// both the link text and its only child.
func autolink(text, dest string) Inline {
	return Inline{Kind: InlineLink, Text: text, Dest: dest, Children: []Inline{{Kind: InlineText, Text: text}}}
}

// pi v1.4.0 packages/tui/src/components/markdown.ts configures Marked 18.0.11
// with its default gfm inline rules, whose `url` tokenizer links bare URLs and
// email addresses and post-processes them with `_backpedal`. These cases were
// generated from that parser.
func TestBareAutolinksMatchMarked(t *testing.T) {
	for _, item := range []struct {
		src  string
		want []Inline
	}{
		{"https://example.com", []Inline{autolink("https://example.com", "https://example.com")}},
		{"www.example.com", []Inline{autolink("www.example.com", "http://www.example.com")}},
		{"a@example.com", []Inline{autolink("a@example.com", "mailto:a@example.com")}},
		{"HTTP://Example.COM", []Inline{autolink("HTTP://Example.COM", "HTTP://Example.COM")}},
		{"hTtPs://e.com", []Inline{autolink("hTtPs://e.com", "hTtPs://e.com")}},
		{"FtP://f.com", []Inline{autolink("FtP://f.com", "FtP://f.com")}},
		{"ftp://f.com.", []Inline{autolink("ftp://f.com", "ftp://f.com"), {Kind: InlineText, Text: "."}}},
		// Only the literal lowercase `www.` prefix gets an http:// destination.
		{"WWW.Example.com", []Inline{{Kind: InlineText, Text: "WWW.Example.com"}}},
		{"example.com", []Inline{{Kind: InlineText, Text: "example.com"}}},
		// A URL can start mid-word; there is no word boundary requirement.
		{"xhttps://y.com", []Inline{{Kind: InlineText, Text: "x"}, autolink("https://y.com", "https://y.com")}},
		{"awww.x.com", []Inline{{Kind: InlineText, Text: "a"}, autolink("www.x.com", "http://www.x.com")}},
		// Text after the URL is preserved exactly.
		{"https://x.com\u00a0y", []Inline{autolink("https://x.com", "https://x.com"), {Kind: InlineText, Text: "\u00a0y"}}},
		{"see (https://example.com/a).", []Inline{
			{Kind: InlineText, Text: "see ("},
			autolink("https://example.com/a", "https://example.com/a"),
			{Kind: InlineText, Text: ")."},
		}},
		// _backpedal keeps balanced parentheses and trims unbalanced ones.
		{"https://example.com/a(b)", []Inline{autolink("https://example.com/a(b)", "https://example.com/a(b)")}},
		{"https://example.com/a(b", []Inline{autolink("https://example.com/a", "https://example.com/a"), {Kind: InlineText, Text: "(b"}}},
		{"https://x.com/a)&1;", []Inline{autolink("https://x.com/a", "https://x.com/a"), {Kind: InlineText, Text: ")&1;"}}},
		// A trailing entity-like `&name;` is trimmed; a bare `&` is kept.
		{"https://example.com&1;", []Inline{autolink("https://example.com", "https://example.com"), {Kind: InlineText, Text: "&1;"}}},
		{"https://example.com&", []Inline{autolink("https://example.com&", "https://example.com&")}},
		// Trailing `_` and `.` runs are trimmed, but interior ones survive.
		{"https://x.com/a_b_", []Inline{autolink("https://x.com/a_b", "https://x.com/a_b"), {Kind: InlineText, Text: "_"}}},
		// Email backpedalling cannot drop the only dot group, so it fails whole.
		{"a@b.c-", []Inline{{Kind: InlineText, Text: "a@b.c-"}}},
		{"a@b..c", []Inline{{Kind: InlineText, Text: "a@b..c"}}},
		{"a@b.c.d-", []Inline{autolink("a@b.c", "mailto:a@b.c"), {Kind: InlineText, Text: ".d-"}}},
		{"a@b.c;d", []Inline{autolink("a@b.c", "mailto:a@b.c"), {Kind: InlineText, Text: ";d"}}},
		// The `www.` alternative wins over the email alternative.
		{"www.a@b.com", []Inline{autolink("www.a@b.com", "http://www.a@b.com")}},
		// A URL inside a link label is not a link: Marked's url tokenizer runs
		// only while the lexer is outside a link.
		{"[https://x.com](dest)", []Inline{{Kind: InlineLink, Text: "https://x.com", Dest: "dest", Children: []Inline{{Kind: InlineText, Text: "https://x.com"}}}}},
		{"[www.x.com](dest)", []Inline{{Kind: InlineLink, Text: "www.x.com", Dest: "dest", Children: []Inline{{Kind: InlineText, Text: "www.x.com"}}}}},
		{"![https://x.com](i.png)", []Inline{{Kind: InlineImage, Text: "https://x.com", Dest: "i.png", Children: []Inline{{Kind: InlineText, Text: "https://x.com"}}}}},
		{"[a https://x.com b](outer)", []Inline{{Kind: InlineLink, Text: "a https://x.com b", Dest: "outer", Children: []Inline{{Kind: InlineText, Text: "a https://x.com b"}}}}},
		// A rejected outer link falls back to lexing the raw source, where the
		// URL can swallow the balanced tail.
		{"[a [b](c) https://x.com](outer)", []Inline{
			{Kind: InlineText, Text: "[a "},
			{Kind: InlineLink, Text: "b", Dest: "c", Children: []Inline{{Kind: InlineText, Text: "b"}}},
			{Kind: InlineText, Text: " "},
			autolink("https://x.com](outer)", "https://x.com](outer)"),
		}},
		{"[[https://x.com](inner)](outer)", []Inline{
			{Kind: InlineText, Text: "["},
			autolink("https://x.com", "inner"),
			{Kind: InlineText, Text: "](outer)"},
		}},
		// `<a ` opens an in-link region that suppresses bare URLs until `</a>`.
		{"<a href=\"x\">https://y.com</a>", []Inline{
			{Kind: InlineHTML, Text: "<a href=\"x\">"},
			{Kind: InlineText, Text: "https://y.com"},
			{Kind: InlineHTML, Text: "</a>"},
		}},
		// The start tag requires the attribute space, and an explicit link
		// clears the region state (Marked's outputLink resets inLink).
		{"<a>https://y.com", []Inline{
			{Kind: InlineHTML, Text: "<a>"},
			autolink("https://y.com", "https://y.com"),
		}},
		{"<a href=\"x\">[a](b)https://y.com", []Inline{
			{Kind: InlineHTML, Text: "<a href=\"x\">"},
			{Kind: InlineLink, Text: "a", Dest: "b", Children: []Inline{{Kind: InlineText, Text: "a"}}},
			autolink("https://y.com", "https://y.com"),
		}},
		// URLs inside emphasis are tokenized inside the emphasis content.
		{"*https://x.com*", []Inline{{Kind: InlineEmphasis, Text: "https://x.com", Children: []Inline{autolink("https://x.com", "https://x.com")}}}},
		// Marked tries emStrong before url, so a leading `_` that pairs is
		// emphasis while an unpaired one can begin an email local part.
		{"_@e.f_", []Inline{{Kind: InlineEmphasis, Text: "@e.f", Children: []Inline{{Kind: InlineText, Text: "@e.f"}}}}},
		{"_@e.f", []Inline{autolink("_@e.f", "mailto:_@e.f")}},
		{"__@e.f__", []Inline{{Kind: InlineStrong, Text: "@e.f", Children: []Inline{{Kind: InlineText, Text: "@e.f"}}}}},
	} {
		if got := ParseInline(item.src); !reflect.DeepEqual(got, item.want) {
			t.Errorf("ParseInline(%q) = %+v, want %+v", item.src, got, item.want)
		}
	}
}
