package markdown

import (
	"regexp"
	"strings"
)

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
	blocks := make([]*Block, 0, 16)
	at := from
	for at < to {
		lineStart := at
		ln := nextLine(src, at)
		raw := ln.text
		if isBlankLine(raw) {
			for at < to {
				bl := nextLine(src, at)
				if !isBlankLine(bl.text) {
					break
				}
				at = bl.next
			}
			blocks = append(blocks, &Block{Kind: BlockSpace, Start: lineStart, End: at})
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
	// LaTeX block (the upstream extension runs before the built-ins).
	if isLatexBlockStart(raw) {
		// A block whose body reaches a table start is not math: the opener stays
		// text and the table lexes normally (upstream 2ed3118b0, #11159).
		if b, next, ok := l.lexLatex(start); ok {
			return b, next, true
		}
	}
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
	bodyEnd := len(src)
	closed := false
	for at < len(src) {
		ln := nextLine(src, at)
		if isFenceEnd(ln.text, marker) {
			bodyEnd = at
			closed = true
			break
		}
		at = ln.next
	}
	body := strings.TrimSuffix(src[bodyStart:bodyEnd], "\n")
	end := len(src)
	if closed {
		closeLine := nextLine(src, bodyEnd)
		end = closeLine.next
		// A following blank line owns the closing newline (Marked).
		if end < len(src) && src[end] == '\n' {
			end--
		}
	} else if body != "" && !strings.HasSuffix(src[bodyStart:bodyEnd], "\n") {
		// A partial (newline-less) final line is not part of the text.
		if i := strings.LastIndexByte(body, '\n'); i >= 0 {
			body = body[:i]
		} else {
			body = ""
		}
	}
	return &Block{Kind: BlockCode, Start: start, End: end, Info: info, Body: body, Raw: src[start:end]}, end, true
}

// isLatexBlockStart reports a `$$` or `\[` block opener.
// D228: unlike upstream, this only fires at a line start (after at most three
// spaces), not mid-paragraph. Upstream's block extension `start` hook matches
// any `$$`/`\[` with up to three leading spaces and Marked reads that index as
// the end of the preceding paragraph, so `a $$x^2$$ b` splits; reproducing it
// would also import the quadratic scan that hook costs. See docs/DIVERGENCES.md.
func isLatexBlockStart(line string) bool {
	i, width := skipIndent(line, 4)
	if width >= 4 || i >= len(line) {
		return false
	}
	rest := line[i:]
	return strings.HasPrefix(rest, "$$") || strings.HasPrefix(rest, `\[`)
}

// tableDelimiterRowPattern matches a GFM delimiter row such as `|---|`, `| --- |`
// or `:---:` (upstream TABLE_DELIMITER_ROW_REGEX).
var tableDelimiterRowPattern = regexp.MustCompile(`^ {0,3}\|?[ \t]*:?-+:?[ \t]*(?:\|[ \t]*:?-+:?[ \t]*)*\|?[ \t]*$`)

// containsTableStart reports whether these lines begin a GFM table: a header row
// followed by a delimiter row. A pipe-free header needs a pipe in the delimiter
// row and vice versa, otherwise the delimiter row is a thematic break.
func containsTableStart(lines []string) bool {
	for index := 0; index+1 < len(lines); index++ {
		delimiter := lines[index+1]
		if (strings.Contains(delimiter, "|") || strings.Contains(lines[index], "|")) &&
			tableDelimiterRowPattern.MatchString(delimiter) {
			return true
		}
	}
	return false
}

// lexLatex lexes a `$$…$$` or `\[…\]` block, folding a following blank line
// into its raw like Marked's extension token. ok is false when the body reaches
// a table start, which makes the opener ordinary text.
func (l *blockLexer) lexLatex(start int) (*Block, int, bool) {
	src := l.src
	first := nextLine(src, start)
	trimmed := strings.TrimSpace(first.text)
	var closer string
	var openerLen int
	if strings.HasPrefix(trimmed, "$$") {
		closer, openerLen = "$$", 2
	} else {
		closer, openerLen = `\]`, 2
	}
	body := strings.TrimSpace(trimmed[openerLen:])
	bodyLines := []string{trimmed[openerLen:]}
	at := first.next
	closed := false
	if body != "" && strings.HasSuffix(body, closer) {
		body = strings.TrimSpace(body[:len(body)-len(closer)])
		bodyLines = []string{body}
		closed = true
	}
	if !closed {
		for at < len(src) {
			ln := nextLine(src, at)
			if idx := strings.Index(ln.text, closer); idx >= 0 {
				if body != "" {
					body += "\n"
				}
				body += ln.text[:idx]
				bodyLines = append(bodyLines, ln.text[:idx])
				at = ln.next
				closed = true
				break
			}
			if body != "" {
				body += "\n"
			}
			body += ln.text
			bodyLines = append(bodyLines, ln.text)
			at = ln.next
		}
	}
	if containsTableStart(bodyLines) {
		return nil, start, false
	}
	end := at
	// A single following blank line is absorbed into the raw.
	if next := nextLine(src, end); isBlankLine(next.text) && next.next > end {
		end = next.next
	}
	return &Block{Kind: BlockLatex, Start: start, End: end, Raw: src[start:end], Text: strings.TrimSpace(body), Pending: !closed}, end, true
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
