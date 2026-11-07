package markdown

import "testing"

// Reference: pi v1.5.0 (f9a26365) marked 18.0.11's html block rule, which
// upstream packages/tui/src/components/markdown.ts lexes with. Types 1-5 end
// at their own closing token, not at a blank line:
//
//	<(script|pre|style|textarea)[\s>][\s\S]*?(?:</\1>[^\n]*\n*|$)   type 1
//	comment[^\n]*(\n+|$)                       type 2, comment = <!--...-->
//	<\?[\s\S]*?(?:\?>[^\n]*\n*|$)              type 3
//	<![A-Z][\s\S]*?(?:>[^\n]*\n*|$)            type 4
//	<!\[CDATA\[[\s\S]*?(?:\]\]>[^\n]*\n*|$)    type 5
//
// Types 6 and 7 end at a blank line. Pier ended every type at a blank line, so
// a one-line comment, declaration or <pre> swallowed the Markdown after it.
func TestHTMLBlockEndsAtItsClosingToken(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		src   string
		raw   string
		after []BlockKind
	}{
		{
			name:  "pre",
			src:   "<pre>\ncode\n</pre>\nafter\n",
			raw:   "<pre>\ncode\n</pre>\n",
			after: []BlockKind{BlockParagraph},
		},
		{
			name:  "script",
			src:   "<script>\nx\n</script>\nafter\n",
			raw:   "<script>\nx\n</script>\n",
			after: []BlockKind{BlockParagraph},
		},
		{
			name:  "style",
			src:   "<style>\nx\n</style>\nafter\n",
			raw:   "<style>\nx\n</style>\n",
			after: []BlockKind{BlockParagraph},
		},
		{
			name:  "textarea",
			src:   "<textarea>\nx\n</textarea>\nafter\n",
			raw:   "<textarea>\nx\n</textarea>\n",
			after: []BlockKind{BlockParagraph},
		},
		{
			name:  "comment",
			src:   "<!-- c -->\nafter\n",
			raw:   "<!-- c -->\n",
			after: []BlockKind{BlockParagraph},
		},
		{
			name:  "comment keeps following list",
			src:   "<!-- c -->\n- a\n- b\n",
			raw:   "<!-- c -->\n",
			after: []BlockKind{BlockList},
		},
		{
			name:  "multi-line comment",
			src:   "<!--\na\n-->\nafter\n",
			raw:   "<!--\na\n-->\n",
			after: []BlockKind{BlockParagraph},
		},
		{
			name:  "processing instruction",
			src:   "<?php x ?>\nafter\n",
			raw:   "<?php x ?>\n",
			after: []BlockKind{BlockParagraph},
		},
		{
			name:  "declaration",
			src:   "<!DOCTYPE html>\nafter\n",
			raw:   "<!DOCTYPE html>\n",
			after: []BlockKind{BlockParagraph},
		},
		{
			name:  "cdata",
			src:   "<![CDATA[x]]>\nafter\n",
			raw:   "<![CDATA[x]]>\n",
			after: []BlockKind{BlockParagraph},
		},
		{
			// The closing token takes the rest of its line with it.
			name:  "closer mid-line",
			src:   "<pre>x</pre> after\nnext\n",
			raw:   "<pre>x</pre> after\n",
			after: []BlockKind{BlockParagraph},
		},
		{
			// An unclosed block runs to the end of the source.
			name:  "unclosed comment",
			src:   "<!-- unclosed\nafter\n",
			raw:   "<!-- unclosed\nafter\n",
			after: nil,
		},
		{
			// Types 6 and 7 still end at a blank line.
			name:  "div ends at a blank line",
			src:   "<div>\nx\n</div>\nafter\n",
			raw:   "<div>\nx\n</div>\nafter\n",
			after: nil,
		},
		{
			name:  "complete tag ends at a blank line",
			src:   "<custom-el>\nafter\n",
			raw:   "<custom-el>\nafter\n",
			after: nil,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			blocks := Lex(testCase.src)
			if len(blocks) == 0 || blocks[0].Kind != BlockHTML {
				t.Fatalf("blocks = %v, want a leading HTML block", blockKinds(blocks))
			}
			if got := blocks[0].Raw; got != testCase.raw {
				t.Errorf("html raw = %q, want %q", got, testCase.raw)
			}
			if got := blockKinds(blocks[1:]); len(got) != len(testCase.after) {
				t.Fatalf("blocks after html = %v, want %v", got, testCase.after)
			} else {
				for index := range got {
					if got[index] != testCase.after[index] {
						t.Fatalf("blocks after html = %v, want %v", got, testCase.after)
					}
				}
			}
		})
	}
}

// TestHTMLBlockClosingTokenIsCaseInsensitive: the reference matches type 1 with
// the `i` flag, so `</PRE>` closes `<pre>`.
func TestHTMLBlockClosingTokenIsCaseInsensitive(t *testing.T) {
	blocks := Lex("<PRE>\ncode\n</PRE>\nafter\n")
	if len(blocks) != 2 || blocks[0].Kind != BlockHTML {
		t.Fatalf("blocks = %v, want html then paragraph", blockKinds(blocks))
	}
	if want := "<PRE>\ncode\n</PRE>\n"; blocks[0].Raw != want {
		t.Fatalf("html raw = %q, want %q", blocks[0].Raw, want)
	}
}
