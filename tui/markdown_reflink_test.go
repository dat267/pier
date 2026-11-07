package tui

import (
	"slices"
	"strings"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) packages/tui/src/components/markdown.ts with
// marked 18.0.11. `want` is upstream's Markdown rendered at width 40 with
// mdTestTheme and renderLatex false. Marked keeps the first definition of each
// tag in tokens.links while lexing the blocks and resolves reflink and nolink
// through it afterwards (Lexer.blockTokens, Tokenizer.def and
// Tokenizer.reflink); markdown.ts also clips a paragraph at a LaTeX opener
// line and joins it back when the extension declines the line.
func TestMarkdownReferenceLinksMatchUpstream(t *testing.T) {
	for _, testCase := range []struct {
		name, text string
		want       []string
	}{
		{"a reference link resolves through a definition", "[x]: /u\n\nb [x]", []string{"                                        ", "b <link><u>x</u></link><url> (/u)</url> "}},
		{"the reflink forms, a title, case folding and an undefined label", "[x]: /u \"T\"\n\n[t][x] ![x] [X][] [nope]", []string{"                                        ", "<link><u>t</u></link><url> (/u)</url> x ", "<link><u>X</u></link><url> (/u)</url>   ", "[nope]                                  "}},
		{"a label's whitespace runs collapse", "[a  b]: /ab\n\n[A\n b]", []string{"                                        ", "<link><u>A                              ", " b</u></link><url> (/ab)</url>          "}},
		{"a definition after its use still resolves, and masks emphasis", "*a [x* b]\n\n[x]: /u", []string{"<i>a [x</i> b]                          ", "                                        "}},
		{"a repeated definition is dropped", "# h\n[x]: /u\n> \n[x]: /u", []string{"<h><b><u>h</u></b></h>                  ", "                                        "}},
		{"a dropped first definition in an item leaves its newline as a space", "[x]: /a\n- [x]: /b\n  t", []string{"<lb>- </lb>                             ", "  t                                     "}},
		{"a reference whose text holds a link masks only the inner link", "[x]: /u\n\n*a [b [y] *e][x]* f\n\n[y]: /y", []string{"                                        ", "*a [b <link><u>y</u></link><url>        ", "(/y)</url>                              ", "<i>e]<link><u>x</u></link><url>         ", "(/u)</url></i> f                        ", "                                        "}},
		{"a single-quoted title", "[a]: /u 'q'\n\n[a]", []string{"                                        ", "<link><u>a</u></link><url> (/u)</url>   "}},
		{"a multi-line single-quoted title", "[a]: /u 'q\nr'\n\n[a]", []string{"                                        ", "<link><u>a</u></link><url> (/u)</url>   "}},
		{"a parenthesized title cannot nest", "[a]: /u (p(q))\n\n[a]", []string{"[a]: /u (p(q))                          ", "                                        ", "[a]                                     "}},
		{"a line starting $$ that is not math does not end the paragraph", "text\n$$ plain", []string{"text                                    ", "$$ plain                                "}},
		{"a display-math closer must end its line", "\\[x] [x\\]]\nmore", []string{"\\[x] [x\\]]                              ", "more                                    "}},
	} {
		got := NewMarkdown(testCase.text, 0, 0, mdTestTheme(), nil, MarkdownOptions{RenderLatex: new(false)}).Render(40)
		if !slices.Equal(got, testCase.want) {
			t.Errorf("%s: %q\n got %q\nwant %q", testCase.name, testCase.text, got, testCase.want)
		}
	}
}

// A definition streamed after its use changes how an already rendered block
// resolves (upstream renders without block reuse while tokens.links is
// non-empty, markdown.ts), so an append that adds or changes a definition must
// not reuse the cached lines of the blocks before the re-lex window.
func TestMarkdownStreamedDefinitionRerendersEarlierBlocks(t *testing.T) {
	options := MarkdownOptions{RenderLatex: new(false)}
	streamed := NewMarkdown("", 0, 0, mdTestTheme(), nil, options)
	text := "b [x]\n\nsecond paragraph\n\nthird paragraph\n\n"
	for _, next := range []string{"", "[x]: /u", "v\n", "\nfourth [x]\n"} {
		text += next
		streamed.SetText(text)
		got := streamed.Render(40)
		want := NewMarkdown(text, 0, 0, mdTestTheme(), nil, options).Render(40)
		if !slices.Equal(got, want) {
			t.Fatalf("after %q:\n got %q\nwant %q", next, got, want)
		}
	}
	if got := streamed.Render(40)[0]; got != "b <link><u>x</u></link><url> (/uv)</url>" {
		t.Fatalf("first line %q, want the link to /uv", got)
	}
}

// D398: upstream's latexBlock start hook `(?:^|\n) {0,3}(?:\$\$|\\\[)` runs on
// src.slice(1), so its `^` also matches a paragraph's second character, and
// marked clips the paragraph to its first character and joins the rest back
// with a newline: upstream renders "a $$ b" at width 40 as "a", "" and
// "$$ b", and "x$$ y" as "x" and "$$ y". Pier keeps those paragraphs whole and
// only lets a LaTeX opener at a line start end a paragraph, and a later
// opener does not join earlier paragraphs.
func TestMarkdownKeepsAParagraphWhoseSecondCharacterOpensMath(t *testing.T) {
	options := MarkdownOptions{RenderLatex: new(false)}
	for _, text := range []string{"a $$ b", "x$$ y", "$$$$", "a\\[b"} {
		got := NewMarkdown(text, 0, 0, mdTestTheme(), nil, options).Render(40)
		if len(got) != 1 || strings.TrimRight(got[0], " ") != text {
			t.Errorf("%q: got %q, want the paragraph on one line", text, got)
		}
	}
	// A later display-math block does not join two earlier paragraphs
	// (upstream lexes `a\nb c\n-|-` as one paragraph once `$$x$$` follows).
	without := NewMarkdown("a\nb c\n-|-", 0, 0, mdTestTheme(), nil, options).Render(40)
	with := NewMarkdown("a\nb c\n-|-\n\n$$x$$", 0, 0, mdTestTheme(), nil, options).Render(40)
	if len(with) < len(without) || !slices.Equal(with[:len(without)], without) {
		t.Errorf("with a later math block:\n got %q\nwant the prefix %q", with, without)
	}
}
