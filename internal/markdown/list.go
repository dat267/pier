package markdown

import "strings"

// The list tokenizer is a line-for-line port of marked 18.0.11's
// `Tokenizer.list` (pi v1.5.0, f9a26365, which upstream
// packages/tui/src/components/markdown.ts lexes with).

// The indent-bounded rules marked's item loop reads (`other.*Regex(indent)`),
// as bits of one scan. Each is anchored at `^ {0,n}` with
// n = max(0, min(3, indent - 1)) (cachedIndentRegex).
const (
	listBreakFence      = 1 << iota // (?:```|~~~)
	listBreakHeading                // #
	listBreakHTML                   // <(?:[a-z].*>|!--), case-insensitive
	listBreakBlockquote             // >
	listBreakBullet                 // (?:[*+-]|\d{1,9}[.)])(?:[ \t]|$)
	listBreakHR                     // (?:(?:- *){3,}|(?:_ *){3,}|(?:\* *){3,})$
)

// listIndentKey is cachedIndentRegex's index for an item's content indent.
func listIndentKey(indent int) int {
	return max(0, min(3, indent-1))
}

// listLineBreaks reports which of the item-loop rules match line with at most
// n leading spaces. A hand scan of the same patterns keeps the per-line cost of
// a long list to one pass instead of six regex runs.
func listLineBreaks(line string, n int) int {
	i := 0
	for i < len(line) && line[i] == ' ' {
		i++
	}
	if i > n || i >= len(line) {
		return 0
	}
	rest := line[i:]
	switch c := rest[0]; {
	case c == '`' || c == '~':
		if strings.HasPrefix(rest, "```") || strings.HasPrefix(rest, "~~~") {
			return listBreakFence
		}
	case c == '#':
		return listBreakHeading
	case c == '<':
		if strings.HasPrefix(rest, "<!--") {
			return listBreakHTML
		}
		if len(rest) > 1 && isASCIILetter(rest[1]) && strings.IndexByte(rest[2:], '>') >= 0 {
			return listBreakHTML
		}
	case c == '>':
		return listBreakBlockquote
	case c == '-' || c == '*' || c == '_':
		bits := 0
		if c != '_' && (len(rest) == 1 || rest[1] == ' ' || rest[1] == '\t') {
			bits |= listBreakBullet
		}
		if isListHRRun(rest, c) {
			bits |= listBreakHR
		}
		return bits
	case c == '+':
		if len(rest) == 1 || rest[1] == ' ' || rest[1] == '\t' {
			return listBreakBullet
		}
	case c >= '0' && c <= '9':
		j := 0
		for j < len(rest) && j < 9 && rest[j] >= '0' && rest[j] <= '9' {
			j++
		}
		if j < len(rest) && (rest[j] == '.' || rest[j] == ')') &&
			(j+1 == len(rest) || rest[j+1] == ' ' || rest[j+1] == '\t') {
			return listBreakBullet
		}
	}
	return 0
}

// isListHRRun is `(?:c *){3,}$` from the start of s: only c and spaces, with
// at least three c.
func isListHRRun(s string, c byte) bool {
	count := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case c:
			count++
		case ' ':
		default:
			return false
		}
	}
	return count >= 3
}

// listBullet matches marked's itemRegex prefix `^( {0,3}bull)` followed by a
// space, a tab or the end of the line. It returns cap[1]'s length and the
// bullet (`-`, `*`, `+`, or an ordered number's delimiter `.`/`)`).
func listBullet(line string) (capLen int, bullet byte, ordered bool, ok bool) {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	if i >= len(line) {
		return 0, 0, false, false
	}
	switch c := line[i]; {
	case c == '-' || c == '*' || c == '+':
		bullet = c
		i++
	case c >= '0' && c <= '9':
		j := i
		for j < len(line) && j-i < 9 && line[j] >= '0' && line[j] <= '9' {
			j++
		}
		if j >= len(line) || (line[j] != '.' && line[j] != ')') {
			return 0, 0, false, false
		}
		bullet = line[j]
		ordered = true
		i = j + 1
	default:
		return 0, 0, false, false
	}
	if i < len(line) && line[i] != ' ' && line[i] != '\t' {
		return 0, 0, false, false
	}
	return i, bullet, ordered, true
}

// expandListTabs is marked's helpers.ts `expandTabs`: tabs advance to the next
// multiple of four, counting from the starting column.
func expandListTabs(line string, col int) string {
	if !strings.Contains(line, "\t") {
		return line
	}
	var sb strings.Builder
	for i := 0; i < len(line); i++ {
		if line[i] == '\t' {
			added := 4 - col%4
			sb.WriteString(strings.Repeat(" ", added))
			col += added
			continue
		}
		sb.WriteByte(line[i])
		col++
	}
	return sb.String()
}

// firstNonSpace is JavaScript's `line.search(/[^ ]/)`: -1 when every byte is a
// space.
func firstNonSpace(line string) int {
	for i := 0; i < len(line); i++ {
		if line[i] != ' ' {
			return i
		}
	}
	return -1
}

// sliceFrom is JavaScript's `s.slice(n)` for n >= 0.
func sliceFrom(s string, n int) string {
	if n >= len(s) {
		return ""
	}
	return s[n:]
}

// jsBlank reports `!s.trim()`.
func jsBlank(s string) bool {
	return strings.TrimSpace(s) == ""
}

func (l *blockLexer) lexList(start int) (*Block, int, bool) {
	src := l.src
	first := nextLine(src, start)
	_, bullet, ordered, ok := listBullet(first.text)
	if !ok {
		return nil, start, false
	}
	list := &Block{Kind: BlockList, Start: start, Ordered: ordered}
	if ordered {
		list.StartNumber = orderedStart(first.text)
	}

	list.Blocks = make([]*Block, 0, 4)
	at := start
	for at < len(src) {
		ln := nextLine(src, at)
		capLen, itemBullet, itemOrdered, ok := listBullet(ln.text)
		// itemRegex(bull) takes only this list's bullet: the same character, or
		// for an ordered list the same delimiter after any number.
		if !ok || itemOrdered != ordered || itemBullet != bullet {
			break
		}
		// End the list if the bullet was actually a rule.
		if isThematicBreak(ln.text) {
			break
		}
		itemStart := at
		at = ln.next

		line := expandListTabs(ln.text[capLen:], capLen)
		blankLine := jsBlank(line)
		indent := 0
		var contents itemText
		if blankLine {
			indent = capLen + 1
		} else {
			indent = firstNonSpace(line)
			// Treat indented code (more than four spaces) as one space of indent.
			if indent > 4 {
				indent = 1
			}
			contents.first = line[indent:]
			indent += capLen
		}

		endEarly := false
		// Items begin with at most one blank line.
		if blankLine {
			next := nextLine(src, at)
			if isBlankLine(next.text) {
				at = next.next
				endEarly = true
			}
		}

		if !endEarly {
			key := listIndentKey(indent)
			for at < len(src) {
				next := nextLine(src, at)
				rawLine := next.text
				withoutTabs := strings.ReplaceAll(rawLine, "\t", "    ")
				if listLineBreaks(rawLine, key) != 0 {
					break
				}
				if firstNonSpace(withoutTabs) >= indent || jsBlank(rawLine) {
					contents.add(sliceFrom(withoutTabs, indent))
				} else {
					// Not enough indentation: a lazy paragraph continuation,
					// unless a blank line or another block came before it.
					if blankLine {
						break
					}
					if firstNonSpace(strings.ReplaceAll(line, "\t", "    ")) >= 4 ||
						listLineBreaks(line, key)&(listBreakFence|listBreakHeading|listBreakHR) != 0 {
						break
					}
					contents.add(rawLine)
				}
				blankLine = jsBlank(rawLine)
				at = next.next
				line = sliceFrom(withoutTabs, indent)
			}
		}

		markerStart, _ := skipIndent(ln.text, 4)
		list.Blocks = append(list.Blocks, &Block{
			Kind: BlockListItem, Start: itemStart, End: at,
			Text: contents.String(), Marker: ln.text[markerStart:capLen],
		})
	}
	if len(list.Blocks) == 0 {
		return nil, start, false
	}

	// The final item gives its trailing blank lines back to the outer lexer
	// (marked trims the last item's raw and text).
	last := list.Blocks[len(list.Blocks)-1]
	last.Text = strings.TrimRight(last.Text, " \t\n\r\f\v")
	trimmed := strings.TrimRight(src[last.Start:last.End], " \t\n\r\f\v")
	last.End = last.Start + len(trimmed)
	// The outer lexer's space token for what follows is folded into the list
	// when it is one character long (blockTokens: `token.raw.length === 1`).
	if markedSpacePrefixLen(src[last.End:]) == 1 {
		last.End++
	}

	for _, block := range list.Blocks {
		// marked lexes the item with its checkbox text in place and only
		// places the checkbox when the first token is text or a paragraph;
		// otherwise `[ ]` stays text (`- [ ] t\n===` is a heading "[ ] t").
		block.Blocks = lexItemBlocks(block.Text)
		if checked, rest, ok := stripTaskMarker(block.Text); ok && len(block.Blocks) > 0 && block.Blocks[0].Kind == BlockParagraph {
			first := block.Blocks[0]
			_, first.Text, _ = stripTaskMarker(first.Text)
			block.Task = true
			block.Checked = checked
			block.Text = rest
		}
	}
	// Pier's tight/loose reading is unchanged: a blank line inside the list
	// makes it loose. That covers marked's two rules (an item ending in a blank
	// line before the next item, and a multi-line spacer inside an item) except
	// where marked's heading, code or table token swallows the blank line.
	list.End = last.End
	list.Tight = !containsBlankLine(src[start:list.End])
	for _, b := range list.Blocks {
		b.Tight = list.Tight
	}
	return list, list.End, true
}

// itemText accumulates an item's content, allocating only once the item has
// a second line (most items are one line, a slice of the source).
type itemText struct {
	first string
	sb    strings.Builder
	multi bool
}

func (t *itemText) add(line string) {
	if !t.multi {
		t.sb.Grow(len(t.first) + 1 + len(line) + 64)
		t.sb.WriteString(t.first)
		t.multi = true
	}
	t.sb.WriteByte('\n')
	t.sb.WriteString(line)
}

func (t *itemText) String() string {
	if t.multi {
		return t.sb.String()
	}
	return t.first
}

// containsBlankLine reports a whitespace-only line strictly inside s.
func containsBlankLine(s string) bool {
	s = strings.TrimRight(s, " \t\n")
	for at := 0; at < len(s); {
		ln := nextLine(s, at)
		if at > 0 && isBlankLine(ln.text) {
			return true
		}
		at = ln.next
	}
	return false
}

// markedSpacePrefixLen is the length of marked's space rule match,
// /^(?:[ \t]*(?:\n|$))+/, at the start of s (0 when it does not match).
func markedSpacePrefixLen(s string) int {
	n := 0
	for {
		i := n
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		switch {
		case i < len(s) && s[i] == '\n':
			n = i + 1
		case i == len(s):
			return i
		default:
			return n
		}
	}
}
