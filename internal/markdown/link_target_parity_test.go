package markdown

import (
	"reflect"
	"testing"
)

// pi v1.5.0 (f9a26365) packages/tui/src/components/markdown.ts delegates
// inline links to marked 18.0.11, whose `link` rule is
// `^!?\[(label)\]\(\s*(href)(?:(?:[ \t]+(?:\n[ \t]*)?|\n[ \t]*)(title))?\s*\)`
// with `href = <(?:\\.|[^\n<>\\])+>|[^ \t\n\x00-\x1f]+|(?=\))` and
// `title = "(?:\\"?|[^"\\])*"|'(?:\\'?|[^'\\])*'|\((?:\\\)?|[^)\\])*\)`.
// A target that does not match leaves the whole text literal (A437).
func TestInlineLinkTargetMatchesMarkedGrammar(t *testing.T) {
	for _, item := range []struct {
		input string
		dest  string
		title string
	}{
		{"[t](u)", "u", ""},
		{"[t]( u )", "u", ""},
		{"[t](u \"T\")", "u", "T"},
		{"[t](u 'T')", "u", "T"},
		{"[t](u (T))", "u", "T"},
		{"[t](u 'a\\'b')", "u", "a'b"},
		{"[t](u\n\"T\")", "u", "T"},
		{"[t](<u v>)", "u v", ""},
		{"[t](<u v> \"T\")", "u v", "T"},
		{"[t](<u\\>v>)", "u>v", ""},
		{"[t](<>)", "", ""},
		{"[t]()", "", ""},
		{"[t]( )", "", ""},
		{"[t](u*)", "u*", ""},
	} {
		want := []Inline{{Kind: InlineLink, Text: "t", Dest: item.dest, Title: item.title, Children: []Inline{{Kind: InlineText, Text: "t"}}}}
		if got := ParseInline(item.input); !reflect.DeepEqual(got, want) {
			t.Errorf("ParseInline(%q) = %+v, want %+v", item.input, got, want)
		}
	}
}

// The same grammar rejects a destination with whitespace and a title that is
// not quoted or parenthesized. Pier used to accept both, which turned
// `[docs](https://x/a b)` into a link and dropped ` b` from the rendering.
func TestInlineLinkTargetRejectsUnquotedTitle(t *testing.T) {
	for _, input := range []string{
		"[t](u v)",
		"[t](u \\ v)",
		"[t](<u v>undefined)",
		"[t](<u v>T)",
		"[a](<b>c)",
		"[t](u\nv)",
		"[t](u \"a\"b\")",
	} {
		got := ParseInline(input)
		for _, in := range got {
			if in.Kind == InlineLink || in.Kind == InlineImage {
				t.Errorf("ParseInline(%q) = %+v, want no link", input, got)
				break
			}
		}
	}
}

// Escaped punctuation in a destination or title is unescaped, matching marked's
// `replace(anyPunctuation, '$1')` on both fields (A438). The reference link
// definitions already did this through the same helper.
func TestInlineLinkTargetUnescapesPunctuation(t *testing.T) {
	for _, item := range []struct {
		input string
		dest  string
		title string
	}{
		{"[t](u\\)v)", "u)v", ""},
		{"[t](u\\*v)", "u*v", ""},
		{"[t](u\\\\v)", `u\v`, ""},
		{"[t](<u\\)v>)", "u)v", ""},
		{"[t](u \"a\\)b\")", "u", "a)b"},
		{"![i](u\\)v)", "u)v", ""},
	} {
		kind := InlineLink
		if item.input[0] == '!' {
			kind = InlineImage
		}
		want := []Inline{{Kind: kind, Text: "t", Dest: item.dest, Title: item.title, Children: []Inline{{Kind: InlineText, Text: "t"}}}}
		if item.input[0] == '!' {
			want = []Inline{{Kind: InlineImage, Text: "i", Dest: item.dest, Title: item.title, Children: []Inline{{Kind: InlineText, Text: "i"}}}}
		}
		if got := ParseInline(item.input); !reflect.DeepEqual(got, want) {
			t.Errorf("ParseInline(%q) = %+v, want %+v", item.input, got, want)
		}
	}
}
