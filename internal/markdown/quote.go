package markdown

import (
	"regexp"
	"strings"
)

// The blockquote tokenizer ports marked 18.0.11 (pi v1.5.0, f9a26365, which
// upstream packages/tui/src/components/markdown.ts lexes with) in its two
// steps.
//
// The extent is the rule /^( {0,3}> ?(paragraph|[^\n]*)(?:\n|$))+/ with the
// blockquote `paragraph` pattern: each `>` line with content takes the
// following lines that do not start another block (quoteParagraphInterrupts),
// so a table row, a `<br>` or an indented line continues the quote lazily.
//
// Tokenizer.blockquote then lexes those lines in chunks, each a run of lines
// before the next `>` line that follows a lazy one. Every chunk is lexed as top
// level and appended to the quote's tokens, so a lazy table or HTML line
// becomes a sibling block, while a leading paragraph joins the last paragraph
// (`blockTokens(currentText, tokens, true)`). A chunk that ends in code stops
// the quote there; one that ends in a nested quote or a list gives the
// remaining lines to that block instead.

// quoteHTMLInterrupt is the paragraph pattern's html alternative, anchored at
// the line start with no indent: a type-6 block tag, or a
// script/pre/style/textarea/comment opener. Other tags (`<br>`, `<span>`)
// continue the paragraph.
var quoteHTMLInterrupt = regexp.MustCompile(`^(?:</?(?:` + markedBlockTags + `)(?: +|$|/?>)|<(?:script|pre|style|textarea|!--))`)

// markedBlockTags is marked's `_tag` list of type-6 HTML block tags.
const markedBlockTags = `address|article|aside|base|basefont|blockquote|body|caption` +
	`|center|col|colgroup|dd|details|dialog|dir|div|dl|dt|fieldset|figcaption` +
	`|figure|footer|form|frame|frameset|h[1-6]|head|header|hr|html|iframe` +
	`|legend|li|link|main|menu|menuitem|meta|nav|noframes|ol|optgroup|option` +
	`|p|param|search|section|summary|table|tbody|td|tfoot|th|thead|title` +
	`|tr|track|ul`

// isMarkedQuoteStart is marked's `blockquoteStart` (/^ {0,3}>/): spaces only.
func isMarkedQuoteStart(line string) bool {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	return i < len(line) && line[i] == '>'
}

// quoteParagraphInterrupts reports a line that ends a quote's lazy paragraph:
// the blockquote paragraph pattern's `\n(?!hr|heading|blockquote|fences|list|
// html|[ \t]+\n)[^\n]+` continuation. atEOF is whether the line has no
// trailing newline, where a whitespace-only line still continues.
func quoteParagraphInterrupts(line string, atEOF bool) bool {
	if line == "" {
		return true
	}
	if isBlankLine(line) && !atEOF {
		return true
	}
	if isThematicBreak(line) || quoteHeadingInterrupt(line) || isMarkedQuoteStart(line) {
		return true
	}
	if quoteFenceInterrupt(line) || listLineBreaks(line, 3)&listBreakBullet != 0 {
		return true
	}
	return line[0] == '<' && quoteHTMLInterrupt.MatchString(line)
}

// quoteHeadingInterrupt is ` {0,3}#{1,6}(?:\s|$)`.
func quoteHeadingInterrupt(line string) bool {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	hashes := 0
	for i < len(line) && line[i] == '#' {
		hashes++
		i++
	}
	if hashes == 0 || hashes > 6 {
		return false
	}
	return i == len(line) || strings.IndexByte(" \t\r\f\v", line[i]) >= 0
}

// isSetextUnderlineRun is ` {0,3}(?:=+|-+) *` filling the line.
func isSetextUnderlineRun(line string) bool {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	if i >= len(line) || (line[i] != '=' && line[i] != '-') {
		return false
	}
	c := line[i]
	for i < len(line) && line[i] == c {
		i++
	}
	for i < len(line) && line[i] == ' ' {
		i++
	}
	return i == len(line)
}

// stripMarkedQuoteMarker removes ` {0,3}>[ \t]?` from the start of line.
func stripMarkedQuoteMarker(line string) string {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	if i >= len(line) || line[i] != '>' {
		return line
	}
	i++
	if i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[i:]
}

// quoteFenceInterrupt is the paragraph pattern's fence alternative:
// ` {0,3}(?:`{3,}(?=[^`\n]*(?:\n|$))|~~~)`, so a backtick run counts only when
// the rest of the line has no backtick.
func quoteFenceInterrupt(line string) bool {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	rest := line[i:]
	if strings.HasPrefix(rest, "~~~") {
		return true
	}
	run := 0
	for run < len(rest) && rest[run] == '`' {
		run++
	}
	return run >= 3 && !strings.Contains(rest[run:], "`")
}

// quoteExtent returns the offset after the lines marked's blockquote rule
// matches from start, and whether any of them is a lazy (non-`>`) line.
func quoteExtent(src string, start int) (end int, lazy bool) {
	at := start
	for at < len(src) {
		ln := nextLine(src, at)
		if !isMarkedQuoteStart(ln.text) {
			break
		}
		at = ln.next
		marker := strings.IndexByte(ln.text, '>') + 1
		content := ln.text[marker:]
		if strings.HasPrefix(content, " ") {
			content = content[1:]
		}
		// `[^\n]*` matched an empty line: no paragraph, so no lazy lines.
		if content == "" {
			continue
		}
		for at < len(src) {
			next := nextLine(src, at)
			atEOF := next.next == len(src) && !strings.HasSuffix(src, "\n")
			if quoteParagraphInterrupts(next.text, atEOF) {
				break
			}
			lazy = true
			at = next.next
		}
	}
	return at, lazy
}

func (l *blockLexer) lexQuote(start int) (*Block, int) {
	src := l.src
	end, lazy := quoteExtent(src, start)
	if !lazy {
		// Every line carries a marker: one chunk, lexed once, with no
		// continuation for the post-pass to place.
		var sb strings.Builder
		sb.Grow(end - start)
		for at := start; at < end; {
			ln := nextLine(src, at)
			if at > start {
				sb.WriteByte('\n')
			}
			sb.WriteString(stripMarkedQuoteMarker(ln.text))
			at = ln.next
		}
		text := sb.String()
		return &Block{Kind: BlockQuote, Start: start, End: end, Text: text, Blocks: lexBlocksIn(text)}, end
	}
	// The quote's lines, as a nextLine walk of src[start:end] yields them.
	lines := strings.Split(strings.TrimSuffix(src[start:end], "\n"), "\n")
	blocks, text, kept := lexQuoteChunks(lines)
	if kept < len(lines) {
		// A code block stopped the quote: the remaining lines lex outside it.
		end = start
		for range kept {
			end = nextLine(src, end).next
		}
	}
	return &Block{Kind: BlockQuote, Start: start, End: end, Text: text, Blocks: blocks}, end
}

// lexQuoteChunks runs Tokenizer.blockquote's chunk loop over the quote's
// lines. It returns the quote's blocks, its stripped text (the chunk texts
// joined by newlines), and how many of lines the quote keeps.
func lexQuoteChunks(lines []string) ([]*Block, string, int) {
	var blocks []*Block
	var text quoteText
	size := 0
	for _, line := range lines {
		size += len(line) + 1
	}
	text.sb.Grow(size)
	rest := lines
	// tail is set once rest comes from a re-lexed list: its first element is
	// what follows the list on its last line, not one of lines.
	tail := false
	kept := func() int {
		remaining := len(rest)
		if tail {
			remaining--
		}
		return len(lines) - remaining
	}
	for len(rest) > 0 {
		// A chunk is the lines up to the first non-`>` line after a `>` line.
		inQuote := false
		i := 0
		for ; i < len(rest); i++ {
			if isMarkedQuoteStart(rest[i]) {
				inQuote = true
			} else if inQuote {
				break
			}
		}
		chunk := text.addChunk(rest[:i])
		rest = rest[i:]
		// Every chunk has at least one line, so a leftover tail is consumed.
		tail = false
		blocks = appendQuoteChunk(blocks, lexBlocksIn(chunk), text.chunks > 1)
		if len(rest) == 0 {
			break
		}
		last := blocks[len(blocks)-1]
		switch last.Kind {
		case BlockCode:
			// A continuation cannot follow a code block.
			return blocks, text.String(), kept()
		case BlockQuote:
			// The continuation belongs to the nested quote, which had one
			// marker stripped already, so strip one from it too.
			var sb strings.Builder
			sb.WriteString(blockSource(last, chunk))
			for _, line := range rest {
				sb.WriteByte('\n')
				sb.WriteString(stripMarkedQuoteMarker(line))
			}
			blocks[len(blocks)-1], _ = lexBlocksInQuote(sb.String())
			return blocks, text.String(), len(lines)
		case BlockList:
			// The continuation re-lexes with the list; its leftover lines go on
			// to the next chunk.
			newText := blockSource(last, chunk) + "\n" + strings.Join(rest, "\n")
			relexed := (&blockLexer{src: newText}).lexBlocks(0, len(newText))
			if len(relexed) == 0 || relexed[0].Kind != BlockList {
				return blocks, text.String(), kept()
			}
			list := relexed[0]
			blocks[len(blocks)-1] = list
			// marked splits what follows the list's trimmed raw, so a leftover
			// starts with the rest of the list's last line (usually empty).
			leftover := newText[len(blockSource(list, newText)):]
			if leftover == "" {
				return blocks, text.String(), len(lines)
			}
			rest = strings.Split(leftover, "\n")
			tail = true
		}
	}
	return blocks, text.String(), len(lines)
}

// quoteText is the quote's stripped text: its chunk texts joined by newlines,
// written into one buffer. Each chunk is a slice of that buffer, which stays
// valid as the builder grows (it only appends, and a reallocation leaves the
// earlier bytes in place for the slices still holding them).
type quoteText struct {
	sb     strings.Builder
	chunks int
}

// addChunk appends the chunk text of lines and returns it. A chunk's text:
// blockquoteSetextReplace indents a lazy setext underline (` {0,3}(=+|-+) *`
// after a newline) to four spaces so it stays paragraph text, then
// blockquoteSetextReplace2 strips one marker, ` {0,3}>[ \t]?`, from every
// line.
func (t *quoteText) addChunk(lines []string) string {
	if t.chunks > 0 {
		t.sb.WriteByte('\n')
	}
	t.chunks++
	start := t.sb.Len()
	for i, line := range lines {
		if i > 0 {
			t.sb.WriteByte('\n')
			if isSetextUnderlineRun(line) {
				t.sb.WriteString("    ")
				line = strings.TrimLeft(line, " ")
			}
		}
		t.sb.WriteString(stripMarkedQuoteMarker(line))
	}
	return t.sb.String()[start:]
}

func (t *quoteText) String() string { return t.sb.String() }

// lexBlocksInQuote lexes src, which starts with a `>` line, as one quote.
func lexBlocksInQuote(src string) (*Block, int) {
	return (&blockLexer{src: src}).lexQuote(0)
}

// blockSource is a chunk block's raw source: the slice of the chunk text it
// was lexed from, without a trailing newline (marked's token raw).
func blockSource(b *Block, text string) string {
	if b.Start < 0 || b.End > len(text) || b.Start > b.End {
		return ""
	}
	return strings.TrimRight(text[b.Start:b.End], "\n")
}

// appendQuoteChunk appends a chunk's blocks the way blockTokens(…, true) adds
// them to the quote's tokens: the first paragraph joins a paragraph already
// there, and an indented code block always joins a preceding paragraph. A
// continuation chunk that starts with a single newline (the line a re-lexed
// list leaves) adds no space: marked folds a one-character space token into
// the token before it.
func appendQuoteChunk(blocks, chunk []*Block, continuation bool) []*Block {
	if len(blocks) == 0 {
		// The first chunk's blocks are the quote's tokens as they are.
		return chunk
	}
	clipped := true
	for i, b := range chunk {
		if i == 0 && continuation && b.Kind == BlockSpace && b.End-b.Start == 1 && len(blocks) > 0 {
			continue
		}
		var last *Block
		if len(blocks) > 0 {
			last = blocks[len(blocks)-1]
		}
		switch {
		case b.Kind == BlockParagraph && clipped && last != nil && last.Kind == BlockParagraph:
			last.Text += "\n" + b.Text
			clipped = false
		case b.Kind == BlockParagraph:
			blocks = append(blocks, b)
			clipped = false
		case b.Kind == BlockCode && indentWidth(b.Raw) >= 4 && last != nil && last.Kind == BlockParagraph:
			last.Text += "\n" + b.Body
		case b.Kind == BlockLinkDef && last != nil && last.Kind == BlockParagraph:
			// blockTokens joins a definition after a paragraph as its raw
			// (marked 18.0.11): it is text, not a definition.
			last.Text += "\n" + strings.TrimSuffix(b.Raw, "\n")
		default:
			blocks = append(blocks, b)
		}
	}
	return blocks
}
