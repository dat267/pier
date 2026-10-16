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
	return false
}

func (l *blockLexer) lexParagraph(start, to int) (*Block, int) {
	src := l.src
	at := start
	var sb strings.Builder
	first := true
	for at < to {
		ln := nextLine(src, at)
		if isBlankLine(ln.text) {
			break
		}
		if !first && interruptsParagraph(ln.text) {
			break
		}
		// Setext heading: the following line is ===/---.
		if nxt := nextLine(src, ln.next); at+nxt.next <= to || ln.next < to {
			_ = nxt
		}
		if first && ln.next < to {
			nextText := nextLine(src, ln.next).text
			if level, ok := setextLevel(nextText); ok {
				// Consume the underline as part of the heading.
				headingEnd := nextLine(src, ln.next).next
				return &Block{Kind: BlockHeading, Start: start, End: headingEnd, Level: level, Text: ln.text}, headingEnd
			}
		}
		if !first {
			sb.WriteByte('\n')
		}
		sb.WriteString(ln.text)
		first = false
		at = ln.next
	}
	return &Block{Kind: BlockParagraph, Start: start, End: at, Text: sb.String()}, at
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
	return &Block{Kind: BlockQuote, Start: start, End: at, Blocks: lexBlocksIn(inner)}, at
}

func (l *blockLexer) lexHTML(start int) (*Block, int) {
	src := l.src
	at := start
	var sb strings.Builder
	for at < len(src) {
		ln := nextLine(src, at)
		if isBlankLine(ln.text) && sb.Len() > 0 {
			break
		}
		sb.WriteString(ln.text)
		sb.WriteByte('\n')
		at = ln.next
	}
	return &Block{Kind: BlockHTML, Start: start, End: at, Body: sb.String()}, at
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
	return &Block{Kind: BlockCode, Start: start, End: at, Body: body}, at
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
				// A blank line followed by more content keeps the item open;
				// otherwise the blank ends the list.
				follow := nextLine(src, next.next)
				if isBlankLine(follow.text) || follow.next >= len(src) {
					break
				}
				sawBlank = true
				sb.WriteByte('\n')
				at = next.next
				continue
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
		item := &Block{Kind: BlockListItem, Start: itemStart, End: at, Blocks: lexBlocksIn(sb.String())}
		if sawBlank {
			tight = false
		}
		list.Blocks = append(list.Blocks, item)
	}
	list.Tight = tight
	list.End = at
	return list, at, true
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

// isHTMLBlockStart reports a line starting with an HTML tag or comment.
func isHTMLBlockStart(line string) bool {
	i, width := skipIndent(line, 4)
	if width >= 4 || i >= len(line) || line[i] != '<' {
		return false
	}
	if i+1 >= len(line) {
		return false
	}
	c := line[i+1]
	if c == '/' || c == '!' || c == '?' {
		return true
	}
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
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
