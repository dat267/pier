package markdown

import "strings"

// parseLinkDef is marked 18.0.11's def rule (pi v1.5.0 f9a26365, which
// packages/tui/src/components/markdown.ts lexes with):
//
//	^ {0,3}[label]: " *(?:\n[ \t]*)?" ("<x>" | [^<[whitespace]][^whitespace]*)
//	  ( ( " +" ( "\n[ \t]*" )? | " *\n[ \t]*" ) title )? " *(?: newlines | end )"
//
// It returns the label, the href, the optional title and the offset past the
// definition. ok=false leaves the branch to a later one: the whole trailing
// match is what makes an unquoted word after the URL fail into a paragraph.
func parseLinkDef(src string, start int) (tag, href, title string, end int, ok bool) {
	at := start
	for at < len(src) && src[at] == ' ' && at-start < 3 {
		at++
	}
	if at >= len(src) || src[at] != '[' {
		return "", "", "", 0, false
	}
	labelText, afterLabel, labelOk := linkDefLabel(src, at)
	if !labelOk || afterLabel+1 >= len(src) || src[afterLabel+1] != ':' {
		return "", "", "", 0, false
	}
	tag = labelText
	// ` *`: only spaces may precede the target and a tab stays a paragraph.
	at = afterLabel + 2
	for at < len(src) && src[at] == ' ' {
		at++
	}
	// `(?:\n[ \t]*)?`: the target may start on the next line.
	if at < len(src) && src[at] == '\n' {
		at++
		for at < len(src) && (src[at] == ' ' || src[at] == '\t') {
			at++
		}
	}
	if at >= len(src) || src[at] == ' ' || src[at] == '\t' || src[at] == '\n' {
		return "", "", "", 0, false
	}
	href, at = linkDefHref(src, at)
	if at < 0 {
		return "", "", "", 0, false
	}
	// A title is optional: none leaves the scan right after the href.
	if titleAt, titleText := linkDefTitle(src, at); titleAt >= 0 {
		title, at = titleText, titleAt
	}
	// Trailing ` *(?:\n+|$)`.
	for at < len(src) && src[at] == ' ' {
		at++
	}
	if at < len(src) {
		if src[at] != '\n' {
			return "", "", "", 0, false
		}
		for at < len(src) && src[at] == '\n' {
			at++
		}
	}
	return tag, href, title, at, true
}

// linkDefLabel reads `[label]`'s content from the opening bracket at bracket:
// backslash escapes carry, no other bracket or backslash inside, and the
// reference's `(?!\s*\])` applies once at the content's start, so a label may
// end in whitespace (`[x y ]` keeps it) but not consist of only whitespace.
func linkDefLabel(src string, bracket int) (string, int, bool) {
	start := bracket + 1
	i := start
	whitespaceRun := true
	for i < len(src) {
		switch {
		case src[i] == '\\':
			if i+1 >= len(src) {
				return "", 0, false
			}
			i += 2
			whitespaceRun = false
		case src[i] == '[':
			return "", 0, false
		case src[i] == ']':
			if i == start || whitespaceRun {
				return "", 0, false
			}
			return src[start:i], i, true
		case isJSWhitespaceByte(src[i]):
			i++
		default:
			whitespaceRun = false
			i++
		}
	}
	return "", 0, false
}

func isJSWhitespaceByte(b byte) bool {
	switch b {
	case ' ', '\t', '\v', '\f', '\r', '\n':
		return true
	}
	return false
}

// linkDefHref reads the target: an angle URL whose `>` closes on the same
// line, or a run of non-space bytes. It returns -1 when there is no URL.
func linkDefHref(src string, at int) (string, int) {
	if at >= len(src) {
		return "", -1
	}
	if src[at] == '<' {
		nl := strings.IndexByte(src[at:], '\n')
		gt := strings.IndexByte(src[at:], '>')
		if gt < 0 || (nl >= 0 && gt > nl) {
			return "", -1
		}
		return src[at+1 : at+gt], at + gt + 1
	}
	j := at
	for j < len(src) && src[j] != ' ' && src[j] != '\t' && src[j] != '\n' {
		j++
	}
	if j == at {
		return "", -1
	}
	return src[at:j], j
}

// linkDefTitle reads the optional `(?: +(?:\n[ \t]*)?| *\n[ \t]*)title` group
// and returns the offset after it with the title text, or -1 when no title
// follows (the definition is still valid without one).
func linkDefTitle(src string, at int) (int, string) {
	// Candidates: a title right after one or more spaces, and one after any
	// spaces plus a newline and an indent run.
	candidates := make([]int, 0, 3)
	spaceRun := at
	for spaceRun < len(src) && src[spaceRun] == ' ' {
		spaceRun++
	}
	if spaceRun > at {
		if spaceRun < len(src) && src[spaceRun] != '\n' {
			candidates = append(candidates, spaceRun)
		}
		nlRun := spaceRun
		if nlRun < len(src) && src[nlRun] == '\n' {
			indented := nlRun + 1
			for indented < len(src) && (src[indented] == ' ' || src[indented] == '\t') {
				indented++
			}
			candidates = append(candidates, indented)
		}
	} else if at < len(src) && src[at] == '\n' {
		indented := at + 1
		for indented < len(src) && (src[indented] == ' ' || src[indented] == '\t') {
			indented++
		}
		candidates = append(candidates, indented)
	}
	for _, candidate := range candidates {
		if end, closed := defTitle(src, candidate); closed {
			return end, src[candidate:end]
		}
	}
	return -1, ""
}

// defTitle reads `(?:"(?:\\"?|[^"\\])*"|'[^'\n]*(?:\n[^'\n]+)*\n?'|\([^()]*\))`
// and returns the offset after the whole title.
func defTitle(src string, at int) (int, bool) {
	if at >= len(src) {
		return 0, false
	}
	switch src[at] {
	case '"':
		// `\\"?` prefers taking a quote after a backslash; with no closing
		// quote later the regex backtracks to the last such quote.
		lastEscaped := -1
		i := at + 1
		for i < len(src) {
			switch {
			case src[i] == '\\':
				if i+1 < len(src) && src[i+1] == '"' {
					lastEscaped = i + 1
					i += 2
					continue
				}
				i++
			case src[i] == '"':
				return i + 1, true
			default:
				i++
			}
		}
		if lastEscaped >= 0 {
			return lastEscaped + 1, true
		}
	case '\'':
		// `'[^'\n]*(?:\n[^'\n]+)*\n?'`: lines of non-quote text, with no
		// blank line inside.
		i := at + 1
		for {
			for i < len(src) && src[i] != '\'' && src[i] != '\n' {
				i++
			}
			if i >= len(src) {
				return 0, false
			}
			if src[i] == '\'' {
				return i + 1, true
			}
			// A newline: the closing quote, or another non-empty line.
			if i+1 < len(src) && src[i+1] == '\'' {
				return i + 2, true
			}
			if i+1 >= len(src) || src[i+1] == '\n' {
				return 0, false
			}
			i++
		}
	case '(':
		// `\([^()]*\)`: no parenthesis inside.
		for i := at + 1; i < len(src); i++ {
			switch src[i] {
			case ')':
				return i + 1, true
			case '(':
				return 0, false
			}
		}
	}
	return 0, false
}
