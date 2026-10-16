package markdown

import "strings"

// ParseInlines resolves the inline content of every block in the tree.
func ParseInlines(blocks []*Block) {
	for _, b := range blocks {
		parseBlockInlines(b)
	}
}

func parseBlockInlines(b *Block) {
	switch b.Kind {
	case BlockHeading, BlockParagraph:
		b.Inlines = ParseInline(b.Text)
	case BlockTable:
		b.headerInlines = make([][]Inline, len(b.Header))
		for i, cell := range b.Header {
			b.headerInlines[i] = ParseInline(cell)
		}
		b.rowInlines = make([][][]Inline, len(b.Rows))
		for r, row := range b.Rows {
			b.rowInlines[r] = make([][]Inline, len(row))
			for c, cell := range row {
				b.rowInlines[r][c] = ParseInline(cell)
			}
		}
	case BlockQuote, BlockList:
		ParseInlines(b.Blocks)
	}
}

// ParseInline parses a single inline source into inline elements.
func ParseInline(src string) []Inline {
	if src == "" {
		return nil
	}
	var out []Inline
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			out = append(out, Inline{Kind: InlineText, Text: text.String()})
			text.Reset()
		}
	}

	i := 0
	for i < len(src) {
		c := src[i]
		switch c {
		case '\\':
			if i+1 < len(src) {
				if src[i+1] == '\n' {
					flush()
					out = append(out, Inline{Kind: InlineHardBreak})
					i += 2
					continue
				}
				if isASCIIPunct(src[i+1]) {
					text.WriteByte(src[i+1])
					i += 2
					continue
				}
			}
			text.WriteByte(c)
			i++
		case '`':
			j := i
			for j < len(src) && src[j] == '`' {
				j++
			}
			run := j - i
			close := findRun(src, j, '`', run)
			if close >= 0 {
				content := src[j:close]
				flush()
				out = append(out, Inline{Kind: InlineCode, Text: normalizeCodeSpan(content)})
				i = close + run
				continue
			}
			text.WriteString(src[i:j])
			i = j
		case '*', '_':
			consumed, inline, ok := parseDelimited(src, i, c)
			if ok {
				flush()
				out = append(out, inline)
				i = consumed
				continue
			}
			text.WriteByte(c)
			i++
		case '~':
			if i+1 < len(src) && src[i+1] == '~' {
				if close := strings.Index(src[i+2:], "~~"); close >= 0 {
					inner := src[i+2 : i+2+close]
					flush()
					out = append(out, Inline{Kind: InlineStrikethrough, Children: ParseInline(inner)})
					i = i + 2 + close + 2
					continue
				}
			}
			text.WriteByte(c)
			i++
		case '!':
			if i+1 < len(src) && src[i+1] == '[' {
				if inline, consumed, ok := parseLink(src, i, true); ok {
					flush()
					out = append(out, inline)
					i = consumed
					continue
				}
			}
			text.WriteByte(c)
			i++
		case '[':
			if inline, consumed, ok := parseLink(src, i, false); ok {
				flush()
				out = append(out, inline)
				i = consumed
				continue
			}
			text.WriteByte(c)
			i++
		case '<':
			if inline, consumed, ok := parseAngle(src, i); ok {
				flush()
				out = append(out, inline)
				i = consumed
				continue
			}
			text.WriteByte(c)
			i++
		case '\n':
			// Two trailing spaces (or more) make a hard break.
			hard := false
			content := text.String()
			trimmed := strings.TrimRight(content, " ")
			if len(content)-len(trimmed) >= 2 {
				hard = true
				text.Reset()
				text.WriteString(trimmed)
			}
			flush()
			if hard {
				out = append(out, Inline{Kind: InlineHardBreak})
			} else {
				out = append(out, Inline{Kind: InlineSoftBreak})
			}
			i++
		default:
			text.WriteByte(c)
			i++
		}
	}
	flush()
	return out
}

func normalizeCodeSpan(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) >= 2 && s[0] == ' ' && s[len(s)-1] == ' ' && strings.Trim(s, " ") != "" {
		s = s[1 : len(s)-1]
	}
	return s
}

// findRun finds the next run of exactly want copies of ch after from.
func findRun(src string, from int, ch byte, want int) int {
	for i := from; i < len(src); {
		if src[i] != ch {
			i++
			continue
		}
		j := i
		for j < len(src) && src[j] == ch {
			j++
		}
		if j-i == want {
			return i
		}
		i = j
	}
	return -1
}

// parseDelimited parses *emphasis* / **strong** / _emphasis_ starting at i.
func parseDelimited(src string, i int, ch byte) (int, Inline, bool) {
	j := i
	for j < len(src) && src[j] == ch {
		j++
	}
	run := j - i
	if run > 2 {
		run = 2
	}
	// The opening run must be left-flanking: the next byte is not whitespace.
	if j >= len(src) || src[j] == ' ' || src[j] == '\t' || src[j] == '\n' {
		return 0, Inline{}, false
	}
	for closeLen := run; closeLen >= 1; closeLen-- {
		mark := strings.Repeat(string(ch), closeLen)
		closeIdx := findClosing(src, j, mark)
		if closeIdx <= j {
			continue
		}
		inner := src[j:closeIdx]
		if strings.TrimSpace(inner) == "" {
			continue
		}
		kind := InlineEmphasis
		if closeLen == 2 {
			kind = InlineStrong
		}
		return closeIdx + closeLen, Inline{Kind: kind, Children: ParseInline(inner)}, true
	}
	return 0, Inline{}, false
}

// findClosing finds the next run of mark that is right-flanking (preceded by a
// non-space byte), searching from `from`.
func findClosing(src string, from int, mark string) int {
	for i := from; i+len(mark) <= len(src); {
		idx := strings.Index(src[i:], mark)
		if idx < 0 {
			return -1
		}
		at := i + idx
		if at > 0 && src[at-1] != ' ' && src[at-1] != '\t' && src[at-1] != '\n' {
			return at
		}
		i = at + len(mark)
	}
	return -1
}

// parseLink parses `[text](dest "title")` (or the image form when image is
// true, with `[` at i or `![` at i).
func parseLink(src string, i int, image bool) (Inline, int, bool) {
	open := i
	if image {
		open = i + 1
	}
	if open >= len(src) || src[open] != '[' {
		return Inline{}, 0, false
	}
	depth := 0
	end := -1
	for j := open; j < len(src); j++ {
		switch src[j] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				end = j
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 || end+1 >= len(src) || src[end+1] != '(' {
		return Inline{}, 0, false
	}
	close := strings.IndexByte(src[end+2:], ')')
	if close < 0 {
		return Inline{}, 0, false
	}
	target := strings.TrimSpace(src[end+2 : end+2+close])
	dest, title := splitLinkTarget(target)
	label := src[open+1 : end]
	kind := InlineLink
	children := ParseInline(label)
	if image {
		kind = InlineImage
	}
	return Inline{Kind: kind, Children: children, Dest: dest, Title: title}, end + 2 + close + 1, true
}

func splitLinkTarget(target string) (dest, title string) {
	if len(target) == 0 {
		return "", ""
	}
	if target[0] == '<' {
		if end := strings.IndexByte(target, '>'); end >= 0 {
			dest = target[1:end]
			rest := strings.TrimSpace(target[end+1:])
			return dest, strings.Trim(rest, `"'`)
		}
	}
	// dest then optional quoted title.
	for i := 0; i < len(target); i++ {
		if target[i] == ' ' || target[i] == '\t' {
			return target[:i], strings.Trim(strings.TrimSpace(target[i:]), `"'`)
		}
	}
	return target, ""
}

// parseAngle parses an autolink `<scheme:...>` or `<user@host>`. Raw HTML is
// left to the literal text path.
func parseAngle(src string, i int) (Inline, int, bool) {
	end := strings.IndexByte(src[i:], '>')
	if end < 0 {
		return Inline{}, 0, false
	}
	body := src[i+1 : i+end]
	if body == "" {
		return Inline{}, 0, false
	}
	if strings.IndexByte(body, '@') > 0 && strings.IndexByte(body, ' ') < 0 {
		dest := "mailto:" + body
		return Inline{Kind: InlineLink, Children: []Inline{{Kind: InlineText, Text: body}}, Dest: dest, Title: ""}, i + end + 1, true
	}
	if colon := strings.IndexByte(body, ':'); colon > 0 {
		scheme := body[:colon]
		if isASCIIAlpha(scheme) {
			return Inline{Kind: InlineLink, Children: []Inline{{Kind: InlineText, Text: body}}, Dest: body}, i + end + 1, true
		}
	}
	return Inline{}, 0, false
}

func isASCIIAlpha(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return len(s) > 0
}

func isASCIIPunct(c byte) bool {
	return strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", c) >= 0
}
