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
	case BlockListItem:
		for _, child := range b.Blocks {
			parseBlockInlines(child)
		}
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
	// Fast path: plain text with no inline markup is a single text run that can
	// share the source. This is the dominant shape (prose) and skips the text
	// builder entirely.
	if !hasInlineMarkup(src) {
		return []Inline{{Kind: InlineText, Text: src}}
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
				if src[i+1] == '(' {
					if close := strings.Index(src[i+2:], `\)`); close >= 0 {
						inner := src[i+2 : i+2+close]
						flush()
						out = append(out, Inline{Kind: InlineLatex, Raw: src[i : i+2+close+2], Text: inner})
						i = i + 2 + close + 2
						continue
					}
				}
				if isASCIIPunct(src[i+1]) {
					flush()
					out = append(out, Inline{Kind: InlineEscape, Raw: src[i : i+2], Text: src[i+1 : i+2]})
					i += 2
					continue
				}
			}
			text.WriteByte(c)
			i++
		case '\n':
			hard := false
			content := text.String()
			trimmed := strings.TrimRight(content, " ")
			if len(content)-len(trimmed) >= 2 {
				hard = true
				text.Reset()
				text.WriteString(trimmed)
			}
			if hard {
				flush()
				out = append(out, Inline{Kind: InlineHardBreak})
			} else {
				text.WriteByte('\n')
			}
			i++
		case '`':
			j := i
			for j < len(src) && src[j] == '`' {
				j++
			}
			run := j - i
			close := findRun(src, j, '`', run)
			if close >= 0 {
				flush()
				out = append(out, Inline{Kind: InlineCode, Text: normalizeCodeSpan(src[j:close])})
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
					out = append(out, Inline{Kind: InlineStrikethrough, Text: inner, Children: ParseInline(inner)})
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
			if tag, consumed, ok := parseInlineHTML(src, i); ok {
				flush()
				out = append(out, Inline{Kind: InlineHTML, Text: tag})
				i = consumed
				continue
			}
			text.WriteByte(c)
			i++
		case '$':
			if i+1 < len(src) && src[i+1] != '$' {
				if close := findInlineDollar(src, i+1); close >= 0 {
					inner := src[i+1 : close]
					flush()
					out = append(out, Inline{Kind: InlineLatex, Raw: src[i : close+1], Text: inner})
					i = close + 1
					continue
				}
			}
			text.WriteByte(c)
			i++
		default:
			text.WriteByte(c)
			i++
		}
	}
	flush()
	return out
}

// hasInlineMarkup reports whether src contains any byte the inline parser
// treats specially (the fast-path test). A newline is only special when it is a
// hard break (preceded by a space or a backslash); a soft break stays literal.
func hasInlineMarkup(src string) bool {
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '\\', '`', '*', '_', '~', '!', '[', '<', '$':
			return true
		case '\n':
			if i > 0 && (src[i-1] == ' ' || src[i-1] == '\\') {
				return true
			}
		}
	}
	return false
}

// findInlineDollar finds the closing `$` of an inline latex span on the same
// line as the opener.
func findInlineDollar(src string, from int) int {
	for i := from; i < len(src); i++ {
		switch src[i] {
		case '\n':
			return -1
		case '\\':
			i++
		case '$':
			if i+1 < len(src) && src[i+1] == '$' {
				return -1
			}
			return i
		}
	}
	return -1
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

// parseDelimited parses *emphasis* / **strong** / ***both*** / _…_ starting at i.
func parseDelimited(src string, i int, ch byte) (int, Inline, bool) {
	j := i
	for j < len(src) && src[j] == ch {
		j++
	}
	run := j - i
	if run > 3 {
		run = 3
	}
	// An underscore may not open intraword emphasis.
	if ch == '_' && i > 0 && isAlnum(src[i-1]) {
		return 0, Inline{}, false
	}
	if j >= len(src) || src[j] == ' ' || src[j] == '\t' || src[j] == '\n' {
		return 0, Inline{}, false
	}
	closeIdx, closeLen := findClosingRun(src, j, ch, run)
	if closeIdx < 0 {
		return 0, Inline{}, false
	}
	inner := src[j:closeIdx]
	if strings.TrimSpace(inner) == "" {
		return 0, Inline{}, false
	}
	consumed := closeIdx + closeLen
	children := ParseInline(inner)
	switch run {
	case 2:
		return consumed, Inline{Kind: InlineStrong, Text: inner, Children: children}, true
	case 3:
		// ***both*** nests strong inside em; the em's text keeps the strong
		// markers (Marked's inner source).
		strong := Inline{Kind: InlineStrong, Text: inner, Children: children}
		return consumed, Inline{Kind: InlineEmphasis, Text: "**" + inner + "**", Children: []Inline{strong}}, true
	default:
		return consumed, Inline{Kind: InlineEmphasis, Text: inner, Children: children}, true
	}
}

// findClosingRun finds the next run of at least want copies of ch that is
// right-flanking (preceded by a non-space byte).
func findClosingRun(src string, from int, ch byte, want int) (int, int) {
	for i := from; i < len(src); {
		if src[i] != ch {
			i++
			continue
		}
		j := i
		for j < len(src) && src[j] == ch {
			j++
		}
		if j-i >= want && i > 0 && src[i-1] != ' ' && src[i-1] != '\t' && src[i-1] != '\n' {
			return i, j - i
		}
		i = j
	}
	return -1, 0
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
		case '\\':
			j++
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
	if image {
		kind = InlineImage
	}
	return Inline{Kind: kind, Text: label, Children: ParseInline(label), Dest: dest, Title: title}, end + 2 + close + 1, true
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
	for i := 0; i < len(target); i++ {
		if target[i] == ' ' || target[i] == '\t' {
			return target[:i], strings.Trim(strings.TrimSpace(target[i:]), `"'`)
		}
	}
	return target, ""
}

// parseAngle parses an autolink `<scheme:...>` or `<user@host>`.
func parseAngle(src string, i int) (Inline, int, bool) {
	end := strings.IndexByte(src[i:], '>')
	if end < 0 {
		return Inline{}, 0, false
	}
	body := src[i+1 : i+end]
	if body == "" {
		return Inline{}, 0, false
	}
	if strings.IndexByte(body, '@') > 0 && strings.IndexByte(body, ' ') < 0 && strings.IndexByte(body, '<') < 0 {
		dest := "mailto:" + body
		return Inline{Kind: InlineLink, Text: body, Children: []Inline{{Kind: InlineText, Text: body}}, Dest: dest}, i + end + 1, true
	}
	if colon := strings.IndexByte(body, ':'); colon > 0 {
		scheme := body[:colon]
		if isASCIIAlpha(scheme) {
			return Inline{Kind: InlineLink, Text: body, Children: []Inline{{Kind: InlineText, Text: body}}, Dest: body}, i + end + 1, true
		}
	}
	return Inline{}, 0, false
}

// parseInlineHTML parses an inline HTML tag or comment.
func parseInlineHTML(src string, i int) (string, int, bool) {
	if i+1 >= len(src) {
		return "", 0, false
	}
	c := src[i+1]
	if c != '/' && c != '!' && c != '?' && !isASCIILetter(c) {
		return "", 0, false
	}
	end := strings.IndexByte(src[i:], '>')
	if end < 0 {
		return "", 0, false
	}
	return src[i : i+end+1], i + end + 1, true
}

func isASCIIAlpha(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isASCIILetter(s[i]) {
			return false
		}
	}
	return len(s) > 0
}

func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func isASCIIPunct(c byte) bool {
	return strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", c) >= 0
}
