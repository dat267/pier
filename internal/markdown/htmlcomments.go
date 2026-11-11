package markdown

import (
	"regexp"
	"strings"
)

// htmlCommentRegex matches an HTML comment, closed or still being streamed. An
// unclosed comment runs to the end of its block, as in CommonMark, so a
// streamed comment stays hidden.
var htmlCommentRegex = regexp.MustCompile(`(?s)<!--.*?(?:-->|$)`)

// StripHTMLComments removes HTML comments from html source (markdown.ts
// stripHtmlComments).
func StripHTMLComments(text string) string {
	return htmlCommentRegex.ReplaceAllString(text, "")
}

// RemoveHTMLCommentBlocks drops html blocks that hold only comments, so they
// render no line. A dropped comment between two blank lines, or at the start or
// end, also drops one blank line, so it leaves no extra gap (markdown.ts
// removeHtmlCommentBlocks).
func RemoveHTMLCommentBlocks(blocks []*Block) []*Block {
	for i := len(blocks) - 1; i >= 0; i-- {
		switch blocks[i].Kind {
		case BlockList:
			for _, item := range blocks[i].Blocks {
				item.Blocks = RemoveHTMLCommentBlocks(item.Blocks)
			}
		case BlockQuote:
			blocks[i].Blocks = RemoveHTMLCommentBlocks(blocks[i].Blocks)
		case BlockHTML:
			if jsTrim(StripHTMLComments(blocks[i].Raw)) != "" {
				continue
			}
			blocks = append(blocks[:i], blocks[i+1:]...)
			if i < len(blocks) && blocks[i].Kind == BlockSpace && (i == 0 || blocks[i-1].Kind == BlockSpace) {
				blocks = append(blocks[:i], blocks[i+1:]...)
			} else if i == len(blocks) && i > 0 && blocks[i-1].Kind == BlockSpace {
				// The reference decrements its index here as well.
				blocks = append(blocks[:i-1], blocks[i:]...)
				i--
			}
		}
	}
	return blocks
}

// startsWithSpaceOrTab reports the leading whitespace markdown.ts strips after
// a skipped comment.
func startsWithSpaceOrTab(text string) bool {
	return strings.HasPrefix(text, " ") || strings.HasPrefix(text, "\t")
}

// endsWithSpaceOrTab reports whether a token's source ends with whitespace.
func endsWithSpaceOrTab(text string) bool {
	return strings.HasSuffix(text, " ") || strings.HasSuffix(text, "\t")
}
