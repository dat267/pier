package markdown

import (
	"strings"
	"unicode/utf8"
)

// ParseInlines resolves the inline content of every block in the tree. The
// reference lexer keeps its inline flags for the whole document, so `<a `
// regions and link labels span blocks the same way.
func ParseInlines(blocks []*Block) {
	state := &inlineState{}
	parseInlines(blocks, state)
}

func parseInlines(blocks []*Block, state *inlineState) {
	for _, b := range blocks {
		parseBlockInlines(b, state)
	}
}

func parseBlockInlines(b *Block, state *inlineState) {
	switch b.Kind {
	case BlockHeading, BlockParagraph:
		b.Inlines = parseInline(b.Text, state)
	case BlockListItem:
		for _, child := range b.Blocks {
			parseBlockInlines(child, state)
		}
	case BlockTable:
		b.headerInlines = make([][]Inline, len(b.Header))
		for i, cell := range b.Header {
			b.headerInlines[i] = parseInline(cell, state)
		}
		b.rowInlines = make([][][]Inline, len(b.Rows))
		for r, row := range b.Rows {
			b.rowInlines[r] = make([][]Inline, len(row))
			for c, cell := range row {
				b.rowInlines[r][c] = parseInline(cell, state)
			}
		}
	case BlockQuote, BlockList:
		parseInlines(b.Blocks, state)
	}
}

// ParseInline parses a single inline source into inline elements.
func ParseInline(src string) []Inline {
	return parseInline(src, &inlineState{})
}

func parseInline(src string, state *inlineState) []Inline {
	if src == "" {
		return nil
	}
	// Fast path: plain text with no inline markup is a single text run that can
	// share the source. This is the dominant shape (prose) and skips the text
	// builder entirely.
	if !hasInlineMarkup(src) {
		return []Inline{{Kind: InlineText, Text: src}}
	}
	masked := maskEmphasisSource(src)
	var out []Inline
	var text strings.Builder
	var prevText rune
	hasPrevText := false
	flush := func() {
		hasPrevText = false
		if text.Len() > 0 {
			out = append(out, Inline{Kind: InlineText, Text: text.String()})
			text.Reset()
		}
	}

	// Bare autolinks need Marked's ordering: the url tokenizer runs only after
	// every earlier tokenizer declined. Email matches are cached per local-part
	// run so a long word costs one scan, not one per byte.
	emailRunStart, emailRunEnd := 0, 0
	emailRunOK := false
	emailStart := func(i int) bool {
		if i >= emailRunStart && i < emailRunEnd {
			return emailRunOK
		}
		emailRunStart, emailRunEnd, emailRunOK = i, i, false
		if !isEmailLocalChar(src[i]) {
			return false
		}
		end := i
		for end < len(src) && isEmailLocalChar(src[end]) {
			end++
		}
		emailRunEnd = end
		if end < len(src) && src[end] == '@' {
			_, emailRunOK = matchBareEmail(src, i)
		}
		return emailRunOK
	}
	tryAutolink := func(i int) (int, Inline, bool) {
		if state.inLink || i >= len(src) || !canStartBareAutolink(src[i]) {
			return 0, Inline{}, false
		}
		text, dest, length, ok := matchAutolinkPrefixForm(src, i)
		if !ok {
			if !emailStart(i) {
				return 0, Inline{}, false
			}
			end, matched := matchBareEmail(src, i)
			if !matched {
				return 0, Inline{}, false
			}
			text = src[i:end]
			dest = "mailto:" + text
			length = len(text)
		}
		return i + length, Inline{Kind: InlineLink, Text: text, Dest: dest, Children: []Inline{{Kind: InlineText, Text: text}}}, true
	}

	i := 0
	for i < len(src) {
		if text.Len() > 0 {
			value := text.String()
			// Marked updates prevChar only for text not ending in `_`.
			if value[len(value)-1] != '_' {
				prevText, _ = utf8.DecodeLastRuneInString(value)
				hasPrevText = true
			}
		}
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
				if src[i+1] == '(' || src[i+1] == '[' {
					closing := `\)`
					if src[i+1] == '[' {
						closing = `\]`
					}
					if close := findInlineLatexClosing(src, i+2, closing); close >= 0 {
						inner := src[i+2 : close]
						if inner != "" && !strings.Contains(inner, "\n") {
							flush()
							out = append(out, Inline{Kind: InlineLatex, Raw: src[i : close+2], Text: inner})
							i = close + 2
							continue
						}
					} else {
						// An open backslash-delimited math span remains pending.
						flush()
						out = append(out, Inline{Kind: InlineLatex, Raw: src[i:], Text: src[i+2:], Pending: true})
						i = len(src)
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
			consumed, inline, ok := parseEmStrong(src, masked, i, c, prevText, hasPrevText, state)
			if ok {
				flush()
				out = append(out, inline)
				i = consumed
				continue
			}
			// Marked falls through to its url tokenizer after emStrong fails, so
			// `_@e.f` is an email while `_@e.f_` stays emphasis.
			if consumed, inline, ok := tryAutolink(i); ok {
				flush()
				out = append(out, inline)
				i = consumed
				continue
			}
			// Marked retries inside an unmatched run after emitting one marker
			// as text, so `：__1_` can still contain a one-marker opener.
			text.WriteByte(c)
			i++
		case '~':
			if end, inner, ok := strictStrikethrough(src, i); ok {
				flush()
				out = append(out, Inline{Kind: InlineStrikethrough, Text: inner, Children: parseInline(inner, state)})
				i = end
				continue
			}
			end := i + 1
			for end < len(src) && src[end] == '~' {
				end++
			}
			text.WriteString(src[i:end])
			i = end
		case '!':
			if i+1 < len(src) && src[i+1] == '[' {
				if inline, consumed, ok := parseLink(src, i, true, state); ok {
					flush()
					out = append(out, inline)
					i = consumed
					continue
				}
			}
			text.WriteByte(c)
			i++
		case '[':
			if inline, consumed, ok := parseLink(src, i, false, state); ok {
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
				// Marked tracks an open `<a ` region so bare URLs inside it stay
				// text; `</a>` closes it and an explicit link resets it.
				if !state.inLink && hasFoldPrefix(tag, "<a ") {
					state.inLink = true
				} else if state.inLink && hasFoldPrefix(tag, "</a>") {
					state.inLink = false
				}
				i = consumed
				continue
			}
			text.WriteByte(c)
			i++
		case '$':
			opening, closing := 1, "$"
			canOpen := true
			if i+1 < len(src) {
				if src[i+1] == '$' {
					opening, closing = 2, "$$"
				} else {
					next, _ := utf8.DecodeRuneInString(src[i+1:])
					canOpen = !isWhitespaceRune(next)
				}
			}
			if canOpen {
				if close := findInlineLatexClosing(src, i+opening, closing); close >= 0 {
					inner, after := src[i+opening:close], src[close+opening:]
					if inner != "" && !strings.Contains(inner, "\n") && (opening == 2 || !rejectInlineDollar(inner, after)) {
						flush()
						out = append(out, Inline{Kind: InlineLatex, Raw: src[i : close+opening], Text: inner})
						i = close + opening
						continue
					}
				} else if looksLikePendingDollarMath(src[i+opening:]) {
					flush()
					out = append(out, Inline{Kind: InlineLatex, Raw: src[i:], Text: src[i+opening:], Pending: true})
					i = len(src)
					continue
				}
			}
			text.WriteByte(c)
			i++
		default:
			if consumed, inline, ok := tryAutolink(i); ok {
				flush()
				out = append(out, inline)
				i = consumed
				continue
			}
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
	return containsBareAutolink(src)
}

// hasFoldPrefix reports an ASCII case-insensitive prefix match.
func hasFoldPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return strings.EqualFold(s[:len(prefix)], prefix)
}

// findInlineLatexClosing follows markdown.ts findClosingDelimiter: a closing
// delimiter preceded by an odd number of backslashes is escaped.
func findInlineLatexClosing(src string, start int, closing string) int {
	for start < len(src) {
		relative := strings.Index(src[start:], closing)
		if relative < 0 {
			return -1
		}
		index := start + relative
		backslashes := 0
		for previous := index - 1; previous >= 0 && src[previous] == '\\'; previous-- {
			backslashes++
		}
		if backslashes%2 == 0 {
			return index
		}
		start = index + len(closing)
	}
	return -1
}

// rejectInlineDollar mirrors upstream's closed-`$` validity checks
// (packages/tui/src/components/markdown.ts tokenizeInlineLatex): a `$…$` span
// is not math when its inner text ends with whitespace, the character after the
// closing `$` is a digit, the inner text is an identifier followed by a word
// (the currency/constant heuristic), or the inner text contains a backtick. It
// keeps ordinary prose with prices from being detected as math.
func rejectInlineDollar(inner, after string) bool {
	if inner == "" {
		return true
	}
	if last, _ := utf8.DecodeLastRuneInString(inner); isWhitespaceRune(last) {
		return true
	}
	if strings.IndexByte(inner, '`') >= 0 {
		return true
	}
	if after != "" && after[0] >= '0' && after[0] <= '9' {
		return true
	}
	return looksLikeDollarIdentifier(inner) && startsWithWord(after)
}

// looksLikeDollarIdentifier matches upstream's identifier/currency heuristic,
// an uppercase constant such as `X`, `FOO_BAR` or `X+`.
func looksLikeDollarIdentifier(s string) bool {
	if s == "" || !(s[0] == '_' || (s[0] >= 'A' && s[0] <= 'Z')) {
		return false
	}
	i := 0
	for i < len(s) && (s[i] == '_' || (s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	if i == len(s) {
		return true
	}
	// At most one trailing character that is not alphanumeric, `_` or whitespace.
	if i != len(s)-1 {
		return false
	}
	c := s[i]
	return !isAlnum(c) && c != '_' && !isWhitespaceRune(rune(c))
}

// startsWithWord matches upstream's /^[A-Za-z_]/.
func startsWithWord(s string) bool {
	if s == "" {
		return false
	}
	return s[0] == '_' || isASCIILetter(s[0])
}

// looksLikePendingDollarMath reports whether an unterminated `$…` tail looks
// like math (upstream's looksLikePendingDollarMath in
// packages/tui/src/components/markdown.ts): a backslash command or one of the
// operator symbols. It decides whether a streamed `$` becomes a pending LaTeX
// span instead of literal text.
func looksLikePendingDollarMath(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if i+1 < len(s) && isASCIILetter(s[i+1]) {
				return true
			}
		case '_', '^', '=', '+', '*', '/', '<', '>', '(', ')', '[', ']', '|', '-':
			return true
		default:
			if s[i] < utf8.RuneSelf {
				continue
			}
			r, size := utf8.DecodeRuneInString(s[i:])
			i += size - 1
			switch r {
			case '±', '≤', '≥', '≠', '≈', '∈', '→', '⇒', '∞', '∫', '∑', '√':
				return true
			}
		}
	}
	return false
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

// strictStrikethrough matches upstream's StrictStrikethroughTokenizer
// (/^(~~)(?=[^\s~])((?:\\.|[^\\])*?(?:\\.|[^\s~\\]))\1(?=[^~]|$)/): the opener
// must be followed by a non-space, non-tilde character, the content must end
// with one, and the closer must not be followed by another tilde.
func strictStrikethrough(src string, i int) (int, string, bool) {
	const marker = "~~"
	if !strings.HasPrefix(src[i:], marker) {
		return 0, "", false
	}
	start := i + len(marker)
	if start >= len(src) {
		return 0, "", false
	}
	first, _ := utf8.DecodeRuneInString(src[start:])
	if isDelimiterWhitespace(first) || first == '~' {
		return 0, "", false
	}
	lastCanEnd := false
	for position := start; position < len(src); {
		if lastCanEnd && strings.HasPrefix(src[position:], marker) && (position+2 == len(src) || src[position+2] != '~') {
			return position + 2, src[start:position], true
		}
		if src[position] == '\\' {
			if position+1 >= len(src) {
				break
			}
			escaped, size := utf8.DecodeRuneInString(src[position+1:])
			// The reference's \\. atom uses JS dot without the s flag.
			if escaped == '\n' || escaped == '\r' || escaped == '\u2028' || escaped == '\u2029' {
				break
			}
			position += 1 + size
			lastCanEnd = true
		} else {
			current, size := utf8.DecodeRuneInString(src[position:])
			lastCanEnd = !isDelimiterWhitespace(current) && current != '~'
			position += size
		}
	}
	return 0, "", false
}

// parseLink parses `[text](dest "title")` (or the image form when image is
// true, with `[` at i or `![` at i).
func parseLink(src string, i int, image bool, state *inlineState) (Inline, int, bool) {
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
	label := strings.ReplaceAll(strings.ReplaceAll(src[open+1:end], `\[`, "["), `\]`, "]")
	kind := InlineLink
	if image {
		kind = InlineImage
	}
	previous := state.linkEmitted
	state.linkEmitted = false
	state.inLink = true
	children := parseInline(label, state)
	nested := state.linkEmitted
	state.linkEmitted = previous
	// The reference's outputLink leaves inLink false even when it was set by an
	// enclosing `<a ` region.
	state.inLink = false
	if !image {
		if nested {
			return Inline{}, 0, false
		}
		state.linkEmitted = true
	}
	return Inline{Kind: kind, Text: label, Children: children, Dest: dest, Title: title}, end + 2 + close + 1, true
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
	match := angleAutolinkPattern.FindStringSubmatch(src[i:])
	if match == nil {
		return Inline{}, 0, false
	}
	body, dest := match[1], match[1]
	if match[2] == "@" {
		dest = "mailto:" + body
	}
	return Inline{Kind: InlineLink, Text: body, Children: []Inline{{Kind: InlineText, Text: body}}, Dest: dest}, i + len(match[0]), true
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
	tag := inlineHTMLPattern.FindString(src[i:])
	if tag == "" {
		return "", 0, false
	}
	return tag, i + len(tag), true
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
