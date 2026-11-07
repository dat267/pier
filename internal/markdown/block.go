package markdown

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Lex parses src into top-level blocks with their inline content resolved.
// It is the full (non-incremental) entry point.
func Lex(src string) []*Block {
	blocks, _ := LexDefinitions(src, 0, nil)
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
	return l.lexBlocksCap(from, to, 16)
}

// lexBlocksCap is lexBlocks with an initial capacity for the result.
func (l *blockLexer) lexBlocksCap(from, to, capacity int) []*Block {
	return l.lexBlocksMode(from, to, capacity, true)
}

// lexItemBlocks lexes a list item's text the way marked's list tokenizer
// does: with `lexer.state.top = false` (marked 18.0.11 Tokenizer.list, then
// Lexer.blockTokens). Without the top-level paragraph rule, every line is
// tried against the block tokenizers and a plain line is a `text` token that
// joins the text before it. An indented-code line after text joins it with
// the code's de-indented text, and a definition joins it as its raw, so
// `- a\n      c` is the text `a\nc`. blockTokens sets `state.top` back to true
// when it returns, so after a nested list the rest of the item lexes as top
// level again.
func lexItemBlocks(src string) []*Block {
	return (&blockLexer{src: src}).lexBlocksMode(0, len(src), 2, false)
}

// lexBlocksMode lexes src[from:to]; top selects marked's top-level paragraph
// rule over its line-by-line text rule (see lexItemBlocks).
func (l *blockLexer) lexBlocksMode(from, to, capacity int, top bool) []*Block {
	src := l.src
	blocks := make([]*Block, 0, capacity)
	// lastText is set while the last block is a paragraph or text token that
	// indented code or a definition joins (and, below the top, a text line).
	lastText := false
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
			lastText = false
			continue
		}
		if b, next, ok := l.lexBlock(lineStart, raw, ln); ok {
			at = next
			if lastText {
				last := blocks[len(blocks)-1]
				switch {
				case b.Kind == BlockCode && indentWidth(b.Raw) >= 4:
					last.Text += "\n" + b.Body
					last.End = b.End
					continue
				case b.Kind == BlockLinkDef:
					last.Text += "\n" + strings.TrimSuffix(b.Raw, "\n")
					last.End = b.End
					continue
				}
			}
			blocks = append(blocks, b)
			lastText = false
			if b.Kind == BlockList {
				top = true
			}
			continue
		}
		// Paragraph (also handles a setext heading on the next line).
		b, next := l.lexParagraph(lineStart, to)
		if top || b.Kind != BlockParagraph {
			blocks = append(blocks, b)
			// blockTokens joins indented code and a definition into a last
			// paragraph token at any level (marked 18.0.11).
			lastText = b.Kind == BlockParagraph
			at = next
			continue
		}
		// Text: the paragraph's lines up to the first that starts a block of
		// its own, which the top-level paragraph would have continued through.
		// marked also tries lheading at each text line, so an underline after
		// a line its lookahead rejects makes the lines after that one a
		// heading (lexParagraph has already taken the case with none).
		stop := nextLine(src, lineStart).next
		for stop < next {
			inner := nextLine(src, stop)
			if mayStartBlock(inner.text) {
				if _, _, ok := l.lexBlock(stop, inner.text, inner); ok {
					break
				}
			}
			if _, ok := setextLevel(inner.text); ok {
				if from := afterLastLookaheadReject(src, lineStart, stop); from > lineStart && from < stop {
					stop = from
					break
				}
			}
			stop = inner.next
		}
		text := strings.TrimSuffix(src[lineStart:min(stop, next)], "\n")
		if lastText {
			last := blocks[len(blocks)-1]
			last.Text += "\n" + text
			last.End = stop
		} else {
			b.End, b.Text = stop, text
			blocks = append(blocks, b)
		}
		lastText = true
		at = stop
	}
	return blocks
}

// afterLastLookaheadReject returns the offset after the last line in
// src[from:to] that marked's lheading lookahead rejects, or from when it
// accepts them all.
func afterLastLookaheadReject(src string, from, to int) int {
	after := from
	for at := from; at < to; {
		ln := nextLine(src, at)
		if !lheadingLookahead(ln.text) {
			after = ln.next
		}
		at = ln.next
	}
	return after
}

// mayStartBlock is a cheap filter for a paragraph's inner line: no block
// lexBlock knows starts with another character. A table is not among them,
// since lexParagraph already ends before a line with a delimiter row after it.
func mayStartBlock(line string) bool {
	i := 0
	for i < len(line) && line[i] == ' ' {
		i++
	}
	if i >= 4 || i == len(line) {
		return true
	}
	c := line[i]
	return c >= '0' && c <= '9' || strings.IndexByte("\t#`~-*_>+<[$\\", c) >= 0
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
	// Link reference definition (before the table/lheading/paragraph branch,
	// marked 18.0.11 order).
	if tag, href, title, end, ok := parseLinkDef(l.src, start); ok {
		// marked's def tokenizer trims the match's trailing newlines (rtrim in
		// Tokenizer.def, marked 18.0.11) and blockTokens folds a lone "\n"
		// space into it, so the def keeps one newline and the blank lines
		// after it lex as space.
		for end > start+1 && l.src[end-1] == '\n' && l.src[end-2] == '\n' {
			end--
		}
		return &Block{Kind: BlockLinkDef, Start: start, End: end, Text: tag, Href: href, Title: title, Raw: l.src[start:end]}, end, true
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
	// marked's heading rule is /^ {0,3}(#{1,6})(?=\s|$)/: four spaces of
	// indent make indented code instead.
	i, width := skipIndent(line, 4)
	if width >= 4 || i >= len(line) {
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

// isFenceEnd reports a closing fence. Marked's pattern is
// `{0,3}\1[~`]* *(?=\n|$)`: after the opener's own run, any run of backticks and
// tildes, then spaces only. So "```~~~" closes a "```" fence and "```\t" does
// not.
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
	for j < len(line) && (line[j] == '~' || line[j] == '`') {
		j++
	}
	return strings.Trim(line[j:], " ") == ""
}

// fenceIndentCompensation reproduces marked's indentCodeCompensation, which a
// fence tokenizer applies to its body: a backtick fence that starts after
// whitespace removes that many leading whitespace characters from every body
// line that has at least as many. The reference's regex (`/^(\s+)(?:```)/`)
// only matches a backtick fence, so a tilde fence keeps its indentation.
func fenceIndentCompensation(raw, body string) string {
	indent := 0
	for indent < len(raw) && (raw[indent] == ' ' || raw[indent] == '\t') {
		indent++
	}
	if indent == 0 || !strings.HasPrefix(raw[indent:], "```") {
		return body
	}
	lines := strings.Split(body, "\n")
	for index, line := range lines {
		lead := 0
		for _, r := range line {
			if !isWhitespaceRune(r) || lead == indent {
				break
			}
			lead++
		}
		if lead < indent {
			continue
		}
		cut := 0
		for dropped := 0; dropped < indent; dropped++ {
			_, size := utf8.DecodeRuneInString(line[cut:])
			cut += size
		}
		lines[index] = line[cut:]
	}
	return strings.Join(lines, "\n")
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
	body := fenceIndentCompensation(src[start:], strings.TrimSuffix(src[bodyStart:bodyEnd], "\n"))
	end := len(src)
	if closed {
		closeLine := nextLine(src, bodyEnd)
		end = closeLine.next
		// A following blank line owns the closing newline (Marked).
		if end < len(src) && src[end] == '\n' {
			end--
		}
	}
	return &Block{Kind: BlockCode, Start: start, End: end, Info: info, Body: body, Raw: src[start:end]}, end, true
}

// isLatexBlockStart reports a `$$` or `\[` block opener.
// D228: unlike upstream, this only fires at a line start (after at most three
// spaces), not mid-paragraph. Upstream's block extension `start` hook matches
// any `$$`/`\[` with up to three leading spaces and Marked reads that index as
// the end of the preceding paragraph, so `a $$x^2$$ b` splits; reproducing it
// would also import the quadratic scan that hook costs. See DECISIONS.md.
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

// lexLatex is upstream's tokenizeBlockLatex (pi v1.5.0 markdown.ts):
//
//	^ {0,3}\$\$[ \t]*(?:\n)?([\s\S]*?)\$\$[ \t]*(?:\n|$)    (and the \[ \] form)
//
// The closer must end its line, the body must be non-empty and must not reach
// a table start. Otherwise an opener whose rest of the source qualifies is a
// pending block that runs to the end of the source (`\[` always, `$$` only
// when the rest looks like math). A following blank line folds into the raw
// like Marked's one-character space token. ok is false when neither form
// applies, which makes the opener ordinary text.
func (l *blockLexer) lexLatex(start int) (*Block, int, bool) {
	src := l.src
	i := start
	for i < len(src) && i-start < 3 && src[i] == ' ' {
		i++
	}
	closer := `\]`
	if strings.HasPrefix(src[i:], "$$") {
		closer = "$$"
	}
	i += 2
	for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
		i++
	}
	if i < len(src) && src[i] == '\n' {
		i++
	}
	bodyStart := i
	// The lazy body ends at the first closer followed by blanks and a line end.
	for j := bodyStart; j < len(src); {
		k := strings.Index(src[j:], closer)
		if k < 0 {
			break
		}
		pos := j + k
		end := pos + len(closer)
		for end < len(src) && (src[end] == ' ' || src[end] == '\t') {
			end++
		}
		if end < len(src) && src[end] != '\n' {
			j = pos + 1
			continue
		}
		body := src[bodyStart:pos]
		if body == "" || containsTableStart(strings.Split(body, "\n")) {
			break
		}
		if end < len(src) {
			end++
		}
		end = absorbBlankLine(src, end)
		return &Block{Kind: BlockLatex, Start: start, End: end, Raw: src[start:end], Text: jsTrim(body)}, end, true
	}
	rest := src[bodyStart:]
	if rest == "" || (closer == "$$" && !looksLikePendingDollarMath(rest)) || containsTableStart(strings.Split(rest, "\n")) {
		return nil, start, false
	}
	return &Block{Kind: BlockLatex, Start: start, End: len(src), Raw: src[start:], Text: rest, Pending: true}, len(src), true
}

// absorbBlankLine folds one following blank line into a block ending at end.
func absorbBlankLine(src string, end int) int {
	if next := nextLine(src, end); isBlankLine(next.text) && next.next > end {
		return next.next
	}
	return end
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
	contentIndent = min(columnAt(line, i), 24)
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
