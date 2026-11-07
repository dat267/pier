package markdown

import "strings"

// interruptsParagraph reports a line that ends a paragraph (the block starts
// that may interrupt a paragraph in Marked/CommonMark).
func interruptsParagraph(line string) bool {
	if _, _, ok := atxHeading(line); ok {
		return true
	}
	if _, _, ok := fenceStart(line); ok {
		return true
	}
	if isThematicBreak(line) {
		return true
	}
	if isBlockquoteStart(line) {
		return true
	}
	if listMarkerInterrupts(line) {
		return true
	}
	// The html alternative: marked's paragraph pattern continues a line that
	// does not start `</?tag` for a type-6 tag name or one of
	// script/pre/style/textarea/comment, and only at the very line start (its
	// other alternatives carry ` {0,3}` but this one does not, so `  <br>` does
	// not interrupt either). quoteHTMLInterrupt (quote.go) is that test.
	if line != "" && line[0] == '<' && quoteHTMLInterrupt.MatchString(line) {
		return true
	}
	if isLatexBlockStart(line) {
		return true
	}
	return false
}

// listMarkerInterrupts reports a line whose list marker may interrupt a
// paragraph. The reference's paragraph rule is
// ` {0,3}(?:[*+-]|1[.)])[ \t]+[^ \t\n]`, so only an ordered marker numbered 1
// counts, and the marker must be followed by a non-space character: `text\n2. x`
// and `text\n1.` stay one paragraph, `text\n1. x` does not.
func listMarkerInterrupts(line string) bool {
	_, ordered, ok := listItemStart(line)
	if !ok {
		return false
	}
	if ordered {
		// The reference's pattern is `1[.)]`: the literal digit one, so `0. x`
		// does not interrupt. orderedStart normalizes 0 to 1 for a list's start
		// number, so the digits are read here instead.
		markerStart, _ := skipIndent(line, 4)
		markerEnd := markerStart
		for markerEnd < len(line) && line[markerEnd] >= '0' && line[markerEnd] <= '9' {
			markerEnd++
		}
		if line[markerStart:markerEnd] != "1" {
			return false
		}
	}
	// contentIndent is a visual column, so the marker end is what bounds the
	// content. A marker followed only by spaces has no content to interrupt.
	markerEnd := markerEndIndex(line)
	return strings.Trim(line[min(markerEnd, len(line)):], " \t") != ""
}

func (l *blockLexer) lexParagraph(start, to int) (*Block, int) {
	src := l.src
	at := start
	count := 0
	for at < to {
		ln := nextLine(src, at)
		if isBlankLine(ln.text) {
			break
		}
		if count > 0 {
			// A setext underline turns the whole paragraph into a heading.
			if level, ok := setextLevel(ln.text); ok {
				headingEnd := ln.next
				text := strings.TrimSuffix(src[start:at], "\n")
				return &Block{Kind: BlockHeading, Start: start, End: headingEnd, Level: level, Text: text}, headingEnd
			}
			// A table header row interrupts the paragraph.
			if l.looksLikeTableStart(at) {
				break
			}
			if interruptsParagraph(ln.text) {
				break
			}
		}
		count++
		at = ln.next
	}
	// The paragraph text is the source slice (the source already carries the
	// line separators), so it allocates nothing.
	text := strings.TrimSuffix(src[start:at], "\n")
	return &Block{Kind: BlockParagraph, Start: start, End: at, Text: text}, at
}

// looksLikeTableStart reports a header row followed by a delimiter row.
func (l *blockLexer) looksLikeTableStart(at int) bool {
	header := nextLine(l.src, at)
	delim := nextLine(l.src, header.next)
	if delim.next <= header.next {
		return false
	}
	if !tableStartRows(header.text, delim.text) {
		return false
	}
	_, ok := parseDelimiterRow(delim.text)
	return ok
}

// tableStartRows reports whether a header row and a delimiter row start a table.
// A pipe-free header still starts one when the delimiter row has a pipe; with a
// pipe on neither row the delimiter row is a setext underline or a thematic
// break instead (Marked's table rule, upstream 2ed3118b0's case).
func tableStartRows(header string, delimiter string) bool {
	return strings.Contains(header, "|") || strings.Contains(delimiter, "|")
}

func setextLevel(line string) (int, bool) {
	i, width := skipIndent(line, 4)
	if width >= 4 || i >= len(line) {
		return 0, false
	}
	ch := line[i]
	if ch != '=' && ch != '-' {
		return 0, false
	}
	j := i
	for j < len(line) && line[j] == ch {
		j++
	}
	if strings.Trim(line[j:], " \t") != "" {
		return 0, false
	}
	if ch == '=' {
		return 1, true
	}
	// A `-` run is a thematic break if it is 3+ and the whole line is dashes;
	// Marked treats a 1-2 dash underline as setext. A 3+ dash line after a
	// paragraph is a setext h2 as well.
	return 2, true
}

func (l *blockLexer) lexHTML(start int) (*Block, int) {
	src := l.src
	if closer := htmlBlockCloser(nextLine(src, start).text); closer != "" {
		// Types 1-5 end at their own closing token, which takes the rest of
		// its line with it; an unclosed block runs to the end of the source.
		end := len(src)
		if index := closingTokenIndex(src, closer, start); index >= 0 {
			end = nextLine(src, index).next
		}
		return &Block{Kind: BlockHTML, Start: start, End: end, Raw: src[start:end]}, end
	}
	at := start
	for at < len(src) {
		ln := nextLine(src, at)
		if isBlankLine(ln.text) && at > start {
			break
		}
		at = ln.next
	}
	return &Block{Kind: BlockHTML, Start: start, End: at, Raw: src[start:at]}, at
}

// htmlBlockCloser returns the token that ends a CommonMark HTML block of types
// 1-5, or "" for types 6 and 7, which Marked ends at a blank line: a script,
// pre, style or textarea element, a comment, a processing instruction, a
// declaration and a CDATA section. The reference's html rule carries the `i`
// flag, so the element names match case-insensitively.
func htmlBlockCloser(line string) string {
	i, width := skipIndent(line, 4)
	if width >= 4 || i >= len(line) || line[i] != '<' {
		return ""
	}
	rest := line[i+1:]
	switch {
	case strings.HasPrefix(rest, "!--"):
		return "-->"
	case strings.HasPrefix(rest, "?"):
		return "?>"
	case hasPrefixFold(rest, "![CDATA["):
		return "]]>"
	case rest != "" && rest[0] == '!' && len(rest) > 1 && isASCIILetter(rest[1]):
		return ">"
	}
	for _, name := range []string{"script", "pre", "style", "textarea"} {
		if !hasPrefixFold(rest, name) || len(rest) == len(name) {
			continue
		}
		if c := rest[len(name)]; c == ' ' || c == '\t' || c == '>' {
			return "</" + name + ">"
		}
	}
	return ""
}

// closingTokenIndex finds token in src at or after from. The element closers
// need case-insensitive matching (</PRE> closes <pre>); the punctuation-only
// ones do not.
func closingTokenIndex(src, token string, from int) int {
	if !strings.ContainsAny(token, "abcdefghijklmnopqrstuvwxyz") {
		if index := strings.Index(src[from:], token); index >= 0 {
			return from + index
		}
		return -1
	}
	for i := from; i+len(token) <= len(src); i++ {
		if hasPrefixFold(src[i:], token) {
			return i
		}
	}
	return -1
}

// hasPrefixFold reports a case-insensitive ASCII prefix match.
func hasPrefixFold(value, prefix string) bool {
	if len(value) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		left, right := value[i], prefix[i]
		if 'A' <= left && left <= 'Z' {
			left += 'a' - 'A'
		}
		if 'A' <= right && right <= 'Z' {
			right += 'a' - 'A'
		}
		if left != right {
			return false
		}
	}
	return true
}

func (l *blockLexer) lexIndentedCode(start int) (*Block, int) {
	src := l.src
	at := start
	type codeLine struct {
		text string
		end  int
	}
	var lines []codeLine
	for at < len(src) {
		ln := nextLine(src, at)
		if !isBlankLine(ln.text) && indentWidth(ln.text) < 4 {
			break
		}
		lines = append(lines, codeLine{ln.text, ln.next})
		at = ln.next
	}
	// The reference runs the raw through `te` (marked 18.0.11): trailing
	// blankish pieces are cut from the raw when two or more of them follow the
	// last content line. The pieces are the lines plus the empty piece that a
	// final newline contributes, so `    code\n \n` (a whitespace-only line
	// plus the final newline) loses both, while a single final newline stays
	// in the text (`    indented\n` keeps `text: "indented\n"`).
	trailing := 0
	for trailing < len(lines) && isBlankLine(lines[len(lines)-1-trailing].text) {
		trailing++
	}
	synthetic := at > start && src[at-1] == '\n'
	pieces := trailing
	if synthetic {
		pieces++
	}
	kept := len(lines)
	end := at
	var body string
	if pieces >= 2 {
		kept -= trailing
		if kept > 0 {
			end = lines[kept-1].end - 1 // without the newline the cut follows
		} else {
			end = start
		}
		for _, line := range lines[:kept] {
			if len(body) > 0 {
				body += "\n"
			}
			body += stripIndent(line.text, 4)
		}
	} else {
		for _, line := range lines {
			if len(body) > 0 {
				body += "\n"
			}
			body += stripIndent(line.text, 4)
		}
		if synthetic {
			body += "\n"
		}
	}
	return &Block{Kind: BlockCode, Start: start, End: end, Body: body, Raw: src[start:end]}, end
}

func stripIndent(line string, width int) string {
	i, consumed := 0, 0
	for i < len(line) && consumed < width {
		switch line[i] {
		case ' ':
			consumed++
		case '\t':
			consumed += 4
		default:
			return line[i:]
		}
		i++
	}
	return line[i:]
}

// lexList lexes a consecutive run of list items.
// stripTaskMarker removes a leading `[ ] ` / `[x] ` task checkbox.
func stripTaskMarker(content string) (task bool, checked bool, rest string, ok bool) {
	i := 0
	for i < len(content) && (content[i] == ' ' || content[i] == '\t') {
		i++
	}
	if i+3 > len(content) || content[i] != '[' || content[i+2] != ']' {
		return false, false, content, false
	}
	mark := content[i+1]
	if mark != ' ' && mark != 'x' && mark != 'X' {
		return false, false, content, false
	}
	after := i + 3
	if after < len(content) && content[after] != ' ' && content[after] != '\t' && content[after] != '\n' {
		return false, false, content, false
	}
	for after < len(content) && (content[after] == ' ' || content[after] == '\t') {
		after++
	}
	return true, mark != ' ', content[after:], true
}

// markerEndIndex returns the index just past a list marker and its following
// spaces.
func markerEndIndex(line string) int {
	i, _ := skipIndent(line, 4)
	c := line[i]
	if c == '-' || c == '+' || c == '*' {
		i++
	} else {
		for i < len(line) && line[i] >= '0' && line[i] <= '9' {
			i++
		}
		i++ // . or )
	}
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
	return i
}

func orderedStart(line string) int {
	i, _ := skipIndent(line, 4)
	n := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		n = n*10 + int(line[i]-'0')
		i++
	}
	if n == 0 {
		n = 1
	}
	return n
}

func lexBlocksIn(src string) []*Block {
	// A list item or quote chunk usually holds one or two blocks; a top-level
	// document's 16-slot start is wasted allocation here.
	return (&blockLexer{src: src}).lexBlocksCap(0, len(src), 2)
}

// isHTMLBlockStart reports a line starting with an HTML tag or comment. An
// autolink (`<http://x>`, `<a@b.com>`) has no valid tag name and is not an HTML
// block.
func isHTMLBlockStart(line string) bool {
	i, width := skipIndent(line, 4)
	if width >= 4 || i >= len(line) || line[i] != '<' {
		return false
	}
	if i+1 >= len(line) {
		return false
	}
	c := line[i+1]
	if c == '!' || c == '?' {
		return true
	}
	j := i + 1
	if c == '/' {
		j++
	}
	if j >= len(line) || !isASCIILetter(line[j]) {
		return false
	}
	for j < len(line) && (isAlnum(line[j]) || line[j] == '-') {
		j++
	}
	if j >= len(line) {
		return true
	}
	switch line[j] {
	case ' ', '\t', '>', '/':
		return true
	}
	return false
}

// tryTable detects a GFM table: a header row with a `|` followed by a
// delimiter row of dashes and colons.
func (l *blockLexer) tryTable(start int) (*Block, bool) {
	src := l.src
	header := nextLine(src, start)
	delim := nextLine(src, header.next)
	if delim.next <= header.next {
		return nil, false
	}
	if !tableStartRows(header.text, delim.text) {
		return nil, false
	}
	aligns, ok := parseDelimiterRow(delim.text)
	if !ok {
		return nil, false
	}
	table := &Block{Kind: BlockTable, Start: start, Align: aligns}
	table.Header = tableRowCells(header.text, len(aligns))
	at := delim.next
	for at < len(src) {
		ln := nextLine(src, at)
		// A table body ends at a blank line or at another block; any other line
		// is a row, even without a pipe, and its cells are fitted to the header
		// count (Marked's table rule; verified against marked 18.0.11).
		if isBlankLine(ln.text) || startsOtherBlock(ln.text) {
			break
		}
		table.Rows = append(table.Rows, tableRowCells(ln.text, len(aligns)))
		at = ln.next
	}
	table.End = at
	table.Raw = src[start:at]
	return table, true
}

// startsOtherBlock reports whether a line starts a block that ends a table
// body: a heading, fence, thematic break, blockquote, list, HTML block or
// indented code.
func startsOtherBlock(line string) bool {
	if _, _, ok := atxHeading(line); ok {
		return true
	}
	if _, _, ok := fenceStart(line); ok {
		return true
	}
	if isThematicBreak(line) || isBlockquoteStart(line) || isHTMLBlockStart(line) || indentWidth(line) >= 4 {
		return true
	}
	_, _, ok := listItemStart(line)
	return ok
}

// tableRowCells splits a table row and fits it to the table's column count the
// way Marked does: extra cells are dropped and missing ones are empty.
func tableRowCells(line string, columns int) []string {
	cells := splitTableRow(line)
	if len(cells) > columns {
		return cells[:columns]
	}
	for len(cells) < columns {
		cells = append(cells, "")
	}
	return cells
}

func parseDelimiterRow(line string) ([]Align, bool) {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	if len(parts) == 0 {
		return nil, false
	}
	aligns := make([]Align, 0, len(parts))
	for _, part := range parts {
		cell := strings.TrimSpace(part)
		if cell == "" {
			return nil, false
		}
		left := strings.HasPrefix(cell, ":")
		right := strings.HasSuffix(cell, ":")
		mid := strings.Trim(cell, ":")
		if mid == "" {
			return nil, false
		}
		for i := 0; i < len(mid); i++ {
			if mid[i] != '-' {
				return nil, false
			}
		}
		switch {
		case left && right:
			aligns = append(aligns, AlignCenter)
		case left:
			aligns = append(aligns, AlignLeft)
		case right:
			aligns = append(aligns, AlignRight)
		default:
			aligns = append(aligns, AlignNone)
		}
	}
	return aligns, true
}

func splitTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}
