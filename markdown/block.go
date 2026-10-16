package markdown

import "strings"

// Lex parses src into top-level blocks with their inline content resolved.
// It is the full (non-incremental) entry point.
func Lex(src string) []*Block {
	l := &blockLexer{src: src}
	blocks := l.lexBlocks(0, len(src))
	ParseInlines(blocks)
	return blocks
}

// line is one source line. text excludes the terminator; next is the offset of
// the following line (len(src) at EOF).
type line struct {
	text string
	next int
}

func (l *line) hasNewline(src string) bool {
	return l.next > 0 && l.next <= len(src) && src[l.next-1] == '\n'
}

func nextLine(src string, at int) line {
	if at >= len(src) {
		return line{text: "", next: at}
	}
	i := strings.IndexByte(src[at:], '\n')
	if i < 0 {
		return line{text: src[at:], next: len(src)}
	}
	return line{text: src[at : at+i], next: at + i + 1}
}

// indentWidth returns how many spaces a line's leading whitespace represents
// (a tab counts as four, matching the upstream tab expansion).
func indentWidth(s string) int {
	width := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ':
			width++
		case '\t':
			width += 4
		default:
			return width
		}
	}
	return width
}

func isBlankLine(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' {
			return false
		}
	}
	return true
}

// skipIndent advances past up to max columns of spaces/tabs and reports the
// index reached and the width consumed (stops early when the width is met).
func skipIndent(s string, max int) (int, int) {
	i, width := 0, 0
	for i < len(s) && width < max {
		switch s[i] {
		case ' ':
			width++
		case '\t':
			width += 4
		default:
			return i, width
		}
		i++
	}
	return i, width
}

type blockLexer struct {
	src string
}

// lexBlocks lexes src[from:to] into blocks.
func (l *blockLexer) lexBlocks(from, to int) []*Block {
	src := l.src
	var blocks []*Block
	at := from
	for at < to {
		lineStart := at
		ln := nextLine(src, at)
		raw := ln.text
		if isBlankLine(raw) {
			at = ln.next
			continue
		}
		if b, next, ok := l.lexBlock(lineStart, raw, ln); ok {
			blocks = append(blocks, b)
			at = next
			continue
		}
		// Paragraph (also handles a setext heading on the next line).
		b, next := l.lexParagraph(lineStart, to)
		blocks = append(blocks, b)
		at = next
	}
	return blocks
}

// lexBlock tries the single-line block starts in Marked's order. It returns the
// block and the offset after it, or ok=false to fall through to a paragraph.
func (l *blockLexer) lexBlock(start int, raw string, ln line) (*Block, int, bool) {
	// ATX heading.
	if level, text, ok := atxHeading(raw); ok {
		end := ln.next
		return &Block{Kind: BlockHeading, Start: start, End: end, Level: level, Text: text}, end, true
	}
	// Fenced code.
	if marker, info, ok := fenceStart(raw); ok {
		return l.lexFence(start, marker, info)
	}
	// Thematic break.
	if isThematicBreak(raw) {
		end := ln.next
		return &Block{Kind: BlockThematicBreak, Start: start, End: end}, end, true
	}
	// Blockquote.
	if isBlockquoteStart(raw) {
		b, next := l.lexQuote(start)
		return b, next, true
	}
	// List.
	if _, _, ok := listItemStart(raw); ok {
		b, next, ok := l.lexList(start)
		return b, next, ok
	}
	// HTML block (a tag at the line start).
	if isHTMLBlockStart(raw) {
		b, next := l.lexHTML(start)
		return b, next, true
	}
	// Indented code.
	if indentWidth(raw) >= 4 {
		b, next := l.lexIndentedCode(start)
		return b, next, true
	}
	// Table (a header row followed by a delimiter row).
	if b, ok := l.tryTable(start); ok {
		return b, b.End, true
	}
	return nil, start, false
}

// atxHeading parses `{0,3}#{1,6} text`, stripping an optional closing run of #.
func atxHeading(line string) (level int, text string, ok bool) {
	i, _ := skipIndent(line, 4)
	if i >= len(line) {
		return 0, "", false
	}
	j := i
	for j < len(line) && line[j] == '#' {
		j++
	}
	level = j - i
	if level == 0 || level > 6 {
		return 0, "", false
	}
	if j < len(line) && line[j] != ' ' && line[j] != '\t' {
		return 0, "", false
	}
	body := line[j:]
	// Trailing #s (optionally preceded by a space) are not content.
	body = strings.TrimRight(body, " \t")
	k := len(body)
	for k > 0 && body[k-1] == '#' {
		k--
	}
	if k < len(body) {
		if k == 0 || body[k-1] == ' ' || body[k-1] == '\t' {
			body = body[:k]
		}
	}
	return level, strings.Trim(body, " \t"), true
}

// fenceStart reports a fenced code opening: up to three leading spaces and a
// run of three or more backticks or tildes, then an info string.
func fenceStart(line string) (marker, info string, ok bool) {
	i, width := skipIndent(line, 4)
	if width >= 4 || i >= len(line) {
		return "", "", false
	}
	ch := line[i]
	if ch != '`' && ch != '~' {
		return "", "", false
	}
	j := i
	for j < len(line) && line[j] == ch {
		j++
	}
	if j-i < 3 {
		return "", "", false
	}
	marker = line[i:j]
	rest := line[j:]
	// A backtick fence's info string may not contain a backtick.
	if ch == '`' && strings.IndexByte(rest, '`') >= 0 {
		return "", "", false
	}
	return marker, strings.Trim(rest, " \t"), true
}

// isFenceEnd reports a closing fence: the same character and at least as long
// as the opener, with nothing but whitespace after.
func isFenceEnd(line, marker string) bool {
	i, width := skipIndent(line, 4)
	if width >= 4 || i >= len(line) || line[i] != marker[0] {
		return false
	}
	j := i
	for j < len(line) && line[j] == marker[0] {
		j++
	}
	if j-i < len(marker) {
		return false
	}
	return strings.Trim(line[j:], " \t") == ""
}

func (l *blockLexer) lexFence(start int, marker, info string) (*Block, int, bool) {
	src := l.src
	at := nextLine(src, start).next
	bodyStart := at
	closed := false
	for at < len(src) {
		ln := nextLine(src, at)
		if isFenceEnd(ln.text, marker) {
			closed = true
			at = ln.next
			break
		}
		at = ln.next
	}
	body := src[bodyStart:at]
	if closed {
		// Drop the closing fence line and its newline from the body.
		trimmed := strings.TrimRight(body, "\n")
		nl := strings.LastIndexByte(trimmed, '\n')
		_ = nl
		// Recompute: the closing line is the last line of body.
		lastLineStart := strings.LastIndexByte(trimmed, '\n')
		if lastLineStart >= 0 {
			body = trimmed[:lastLineStart+1]
		} else {
			body = ""
		}
	}
	return &Block{Kind: BlockCode, Start: start, End: at, Info: info, Body: body}, at, true
}

func isThematicBreak(line string) bool {
	i, width := skipIndent(line, 4)
	if width >= 4 || i >= len(line) {
		return false
	}
	ch := line[i]
	if ch != '-' && ch != '*' && ch != '_' {
		return false
	}
	count := 0
	for ; i < len(line); i++ {
		switch line[i] {
		case ch:
			count++
		case ' ', '\t':
		default:
			return false
		}
	}
	return count >= 3
}

func isBlockquoteStart(line string) bool {
	i, width := skipIndent(line, 4)
	if width >= 4 || i >= len(line) {
		return false
	}
	return line[i] == '>'
}

// stripBlockquote removes one `>` marker and an optional following space.
func stripBlockquote(line string) string {
	i, _ := skipIndent(line, 4)
	if i >= len(line) || line[i] != '>' {
		return line
	}
	i++
	if i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[i:]
}

// listItemStart reports a bullet or ordered list marker: the marker width, the
// content indent (marker plus following spaces), and whether it is ordered.
func listItemStart(line string) (contentIndent int, ordered bool, ok bool) {
	i, width := skipIndent(line, 4)
	if width >= 4 || i >= len(line) {
		return 0, false, false
	}
	start := i
	c := line[i]
	if c == '-' || c == '+' || c == '*' {
		i++
	} else if c >= '0' && c <= '9' {
		j := i
		for j < len(line) && j-i < 9 && line[j] >= '0' && line[j] <= '9' {
			j++
		}
		if j >= len(line) || (line[j] != '.' && line[j] != ')') {
			return 0, false, false
		}
		i = j + 1
		ordered = true
	} else {
		return 0, false, false
	}
	// The marker must be followed by a space/tab or end the line.
	if i < len(line) && line[i] != ' ' && line[i] != '\t' {
		return 0, false, false
	}
	// Content indent: the visual column where the item's content starts.
	spaces := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		if line[i] == '\t' {
			spaces += 4
		} else {
			spaces++
		}
		i++
		if spaces >= 5 {
			break
		}
	}
	_ = start
	contentIndent = columnAt(line, i)
	if contentIndent > 24 {
		contentIndent = 24
	}
	return contentIndent, ordered, true
}

// columnAt returns the visual column reached at index i (a tab counts as four).
func columnAt(line string, i int) int {
	col := 0
	for j := 0; j < i && j < len(line); j++ {
		if line[j] == '\t' {
			col += 4
		} else {
			col++
		}
	}
	return col
}
