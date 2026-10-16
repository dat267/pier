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
	if _, _, ok := listItemStart(line); ok {
		return true
	}
	if isHTMLBlockStart(line) {
		return true
	}
	if isLatexBlockStart(line) {
		return true
	}
	return false
}

func (l *blockLexer) lexParagraph(start, to int) (*Block, int) {
	src := l.src
	at := start
	var sb strings.Builder
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
				return &Block{Kind: BlockHeading, Start: start, End: headingEnd, Level: level, Text: sb.String()}, headingEnd
			}
			// A table header row interrupts the paragraph.
			if l.looksLikeTableStart(at) {
				break
			}
			if interruptsParagraph(ln.text) {
				break
			}
		}
		if count > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(ln.text)
		count++
		at = ln.next
	}
	return &Block{Kind: BlockParagraph, Start: start, End: at, Text: sb.String()}, at
}

// looksLikeTableStart reports a header row followed by a delimiter row.
func (l *blockLexer) looksLikeTableStart(at int) bool {
	header := nextLine(l.src, at)
	if !strings.Contains(header.text, "|") {
		return false
	}
	delim := nextLine(l.src, header.next)
	if delim.next <= header.next {
		return false
	}
	_, ok := parseDelimiterRow(delim.text)
	return ok
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

func (l *blockLexer) lexQuote(start int) (*Block, int) {
	src := l.src
	at := start
	var sb strings.Builder
	for at < len(src) {
		ln := nextLine(src, at)
		if !isBlockquoteStart(ln.text) {
			if isBlankLine(ln.text) && isBlockquoteStart(nextLine(src, ln.next).text) {
				sb.WriteByte('\n')
				at = ln.next
				continue
			}
			break
		}
		sb.WriteString(stripBlockquote(ln.text))
		sb.WriteByte('\n')
		at = ln.next
	}
	inner := sb.String()
	quoteEnd := at
	if strings.HasSuffix(inner, "\n") {
		inner = inner[:len(inner)-1]
	}
	return &Block{Kind: BlockQuote, Start: start, End: quoteEnd, Text: inner, Blocks: lexBlocksIn(inner)}, at
}

func (l *blockLexer) lexHTML(start int) (*Block, int) {
	src := l.src
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

func (l *blockLexer) lexIndentedCode(start int) (*Block, int) {
	src := l.src
	at := start
	var sb strings.Builder
	for at < len(src) {
		ln := nextLine(src, at)
		if isBlankLine(ln.text) {
			sb.WriteByte('\n')
			at = ln.next
			continue
		}
		if indentWidth(ln.text) < 4 {
			break
		}
		sb.WriteString(stripIndent(ln.text, 4))
		sb.WriteByte('\n')
		at = ln.next
	}
	body := strings.TrimRight(sb.String(), "\n")
	if body != "" && strings.HasSuffix(src[start:at], "\n") {
		body += "\n"
	}
	return &Block{Kind: BlockCode, Start: start, End: at, Body: body, Raw: src[start:at]}, at
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
func (l *blockLexer) lexList(start int) (*Block, int, bool) {
	src := l.src
	first := nextLine(src, start)
	_, ordered, _ := listItemStart(first.text)
	list := &Block{Kind: BlockList, Start: start, Ordered: ordered}
	if ordered {
		list.StartNumber = orderedStart(first.text)
	}
	at := start
	tight := true
	for at < len(src) {
		ln := nextLine(src, at)
		contentIndent, itemOrdered, ok := listItemStart(ln.text)
		if !ok || itemOrdered != ordered {
			break
		}
		markerEnd := markerEndIndex(ln.text)
		itemStart := at
		var sb strings.Builder
		sb.WriteString(ln.text[markerEnd:])
		sb.WriteByte('\n')
		at = ln.next
		sawBlank := false
		for at < len(src) {
			next := nextLine(src, at)
			if isBlankLine(next.text) {
				follow := nextLine(src, next.next)
				if isBlankLine(follow.text) {
					break
				}
				// Another item of this list: the blank is a loose separator; end
				// the item and let the outer loop start the next one.
				if _, followOrdered, isItem := listItemStart(follow.text); isItem && followOrdered == ordered {
					sawBlank = true
					at = next.next
					break
				}
				// An indented continuation keeps the item open.
				if indentWidth(follow.text) >= contentIndent {
					sawBlank = true
					sb.WriteByte('\n')
					at = next.next
					continue
				}
				break
			}
			if indentWidth(next.text) >= contentIndent {
				sb.WriteString(stripIndent(next.text, contentIndent))
				sb.WriteByte('\n')
				at = next.next
				continue
			}
			// A disallowed lazy continuation: another marker of the list is
			// handled by the outer loop; anything else starts a new block.
			break
		}
		content := strings.TrimRight(sb.String(), "\n")
		if sawBlank && content != "" && !strings.Contains(content, "\n\n") {
			content += "\n"
		}
		item := &Block{Kind: BlockListItem, Start: itemStart, End: at, Text: content}
		if _, checked, rest, ok := stripTaskMarker(content); ok {
			item.Task = true
			item.Checked = checked
			item.Text = rest
		}
		item.Blocks = lexBlocksIn(item.Text)
		if sawBlank {
			tight = false
		}
		list.Blocks = append(list.Blocks, item)
	}
	list.Tight = tight
	for _, item := range list.Blocks {
		item.Tight = tight
	}
	list.End = at
	return list, at, true
}

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
	return (&blockLexer{src: src}).lexBlocks(0, len(src))
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
	if !strings.Contains(header.text, "|") {
		return nil, false
	}
	delim := nextLine(src, header.next)
	if delim.next <= header.next {
		return nil, false
	}
	aligns, ok := parseDelimiterRow(delim.text)
	if !ok {
		return nil, false
	}
	table := &Block{Kind: BlockTable, Start: start, Align: aligns}
	table.Header = splitTableRow(header.text)
	at := delim.next
	for at < len(src) {
		ln := nextLine(src, at)
		if isBlankLine(ln.text) || !strings.Contains(ln.text, "|") {
			break
		}
		table.Rows = append(table.Rows, splitTableRow(ln.text))
		at = ln.next
	}
	table.End = at
	return table, true
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
