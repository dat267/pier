package tui

import (
	"strings"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf), components/markdown.ts HTML_COMMENT_REGEX,
// stripHtmlComments, removeHtmlCommentBlocks and the dropLeadingSpace rule, and
// the upstream assertions in test/markdown.test.ts for issue 11710, ported
// verbatim. An unclosed comment runs to the end of its block, so streamed
// comments stay hidden.
func TestMarkdownHidesHTMLComments(t *testing.T) {
	render := func(source string) []string {
		identity := func(text string) string { return text }
		theme := MarkdownTheme{
			Heading: identity, Link: identity, LinkURL: identity, Code: identity,
			CodeBlock: identity, CodeBlockBorder: identity, Quote: identity,
			QuoteBorder: identity, Hr: identity, ListBullet: identity,
			Bold: identity, Italic: identity, Strikethrough: identity, Underline: identity,
		}
		lines := NewMarkdown(source, 0, 0, theme, nil, MarkdownOptions{}).Render(60)
		for i, line := range lines {
			lines[i] = strings.TrimRight(line, " ")
		}
		return lines
	}

	t.Run("hides block and inline comments without extra spacing", func(t *testing.T) {
		assertMarkdownLines(t, render("## [1.0.0]\n\n<!-- release-notes:start -->\nFixed a bug.\n\nSee <!-- todo: link --> the docs."),
			render("## [1.0.0]\n\nFixed a bug.\n\nSee the docs."))
	})

	t.Run("drops one blank line around comment-only blocks", func(t *testing.T) {
		assertMarkdownLines(t, render("<!-- start -->\n\n## [1.0.0]\n\n<!-- a -->\n\n- item\n\n<!-- b -->\n\n## [0.9.0]\n\n<!-- end -->"),
			render("## [1.0.0]\n\n- item\n\n## [0.9.0]"))
	})

	t.Run("hides multi-line and unclosed comments", func(t *testing.T) {
		assertMarkdownLines(t, render("<!--\nmulti\nline\n-->\ntext"), render("text"))
		assertMarkdownLines(t, render("text\n\n<!-- streaming"), render("text"))
	})

	t.Run("hides comments in blockquotes and list items", func(t *testing.T) {
		assertMarkdownLines(t, render("> q\n>\n> <!-- c -->\n>\n> r\n\n- a\n\n  <!-- c -->\n\n  b\n- <!-- c --> item"),
			render("> q\n>\n> r\n\n- a\n\n  b\n- item"))
	})

	t.Run("keeps other HTML and the text after a comment in a block", func(t *testing.T) {
		assertMarkdownLines(t, render("with <tag> markers"), []string{"with <tag> markers"})
		assertMarkdownLines(t, render("<!-- c --> trailing text"), []string{"trailing text"})
		assertMarkdownLines(t, render("<div>\n<!-- c -->a</div>"), []string{"<div>", "a</div>"})
	})
}

// A comment arrives in pieces while a message streams, so no intermediate
// render may show it (markdown.ts HTML_COMMENT_REGEX: an unclosed comment runs
// to the end of its block).
func TestMarkdownStreamingHTMLCommentStaysHidden(t *testing.T) {
	identity := func(text string) string { return text }
	theme := MarkdownTheme{
		Heading: identity, Link: identity, LinkURL: identity, Code: identity,
		CodeBlock: identity, CodeBlockBorder: identity, Quote: identity,
		QuoteBorder: identity, Hr: identity, ListBullet: identity,
		Bold: identity, Italic: identity, Strikethrough: identity, Underline: identity,
	}
	full := "intro\n\n<!--\nrelease notes\n-->\nafter"
	streaming := NewMarkdown("", 0, 0, theme, nil, MarkdownOptions{})
	for n := 1; n <= len(full); n++ {
		streaming.SetText(full[:n])
		for _, line := range streaming.Render(60) {
			if strings.Contains(line, "release notes") || strings.Contains(line, "<!--") {
				t.Fatalf("prefix %q rendered %q", full[:n], line)
			}
		}
	}
	fresh := NewMarkdown(full, 0, 0, theme, nil, MarkdownOptions{})
	streaming.SetText(full)
	assertMarkdownLines(t, streaming.Render(60), fresh.Render(60))
}
