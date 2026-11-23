package markdown

import (
	"slices"
	"unicode/utf8"
)

// parseLinkDef is marked 18.0.11's def rule (pi v1.6.0 faac5858, which
// packages/tui/src/components/markdown.ts lexes with):
//
//	^ {0,3}[label]: " *(?:\n[ \t]*)?" ("<.*?>" | [^<\s][^\s]*)
//	  ( ( " +" ( "\n[ \t]*" )? | " *\n[ \t]*" ) title )? " *(?: newlines | end )"
//
// with JavaScript's `\s`, and `.` stopping at line terminators. It returns the
// label, the href, the optional title and the offset past the definition.
// ok=false leaves the branch to a later one. The regex backtracks when the
// trailing ` *(?:\n+|$)` fails: through the closing quotes a `"` title could
// take, then to no title, then to a longer angle href. The candidates are
// tried in that order, so `[a]: /u\n't'x` is a definition followed by text.
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
	if at >= len(src) || jsSpaceAt(src, at) {
		return "", "", "", 0, false
	}
	for _, hrefEnd := range linkDefHrefEnds(src, at) {
		href = src[at:hrefEnd]
		if src[at] == '<' {
			href = src[at+1 : hrefEnd-1]
		}
		if titleAt := linkDefTitleStart(src, hrefEnd); titleAt >= 0 {
			for _, titleEnd := range defTitleEnds(src, titleAt) {
				if end, ok := linkDefTrailing(src, titleEnd); ok {
					return tag, href, src[titleAt:titleEnd], end, true
				}
			}
		}
		if end, ok := linkDefTrailing(src, hrefEnd); ok {
			return tag, href, "", end, true
		}
	}
	return "", "", "", 0, false
}

// linkDefTrailing matches ` *(?:\n+|$)` at at and returns the offset past it.
func linkDefTrailing(src string, at int) (int, bool) {
	for at < len(src) && src[at] == ' ' {
		at++
	}
	if at == len(src) {
		return at, true
	}
	if src[at] != '\n' {
		return 0, false
	}
	for at < len(src) && src[at] == '\n' {
		at++
	}
	return at, true
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
		case jsSpaceAt(src, i):
			_, size := utf8.DecodeRuneInString(src[i:])
			i += size
		default:
			whitespaceRun = false
			i++
		}
	}
	return "", 0, false
}

// linkDefHrefEnds returns the ends the target can take, in the order the
// regex tries them: `<.*?>` is lazy, so each `>` before the line ends (`.` stops
// at \n, \r, U+2028 and U+2029), and `[^<\s][^\s]*` is greedy, where a shorter
// match leaves a character no later part accepts, so only the longest counts.
func linkDefHrefEnds(src string, at int) []int {
	if src[at] == '<' {
		var ends []int
		for i := at + 1; i < len(src); {
			r, size := utf8.DecodeRuneInString(src[i:])
			if r == '\n' || r == '\r' || r == '\u2028' || r == '\u2029' {
				break
			}
			i += size
			if r == '>' {
				ends = append(ends, i)
			}
		}
		return ends
	}
	j := at
	for j < len(src) && !jsSpaceAt(src, j) {
		_, size := utf8.DecodeRuneInString(src[j:])
		j += size
	}
	return []int{j}
}

// linkDefTitleStart matches `(?: +(?:\n[ \t]*)?| *\n[ \t]*)` after the href
// and returns where a title would start, or -1. A title cannot start with a
// space or a newline, so the alternatives never offer two starts.
func linkDefTitleStart(src string, at int) int {
	i := at
	for i < len(src) && src[i] == ' ' {
		i++
	}
	if i < len(src) && src[i] == '\n' {
		i++
		for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
			i++
		}
		return i
	}
	if i == at {
		return -1
	}
	return i
}

// defTitleEnds matches `(?:"(?:\\"?|[^"\\])*"|'[^'\n]*(?:\n[^'\n]+)*\n?'|\([^()]*\))`
// at at and returns the offsets after each title it can match, in the order the
// regex tries them. A `"` title can close at any quote it reaches, and `\\"?`
// prefers taking a quote after a backslash, so the latest close comes first; the
// other two close at their first delimiter.
func defTitleEnds(src string, at int) []int {
	if at >= len(src) {
		return nil
	}
	switch src[at] {
	case '"':
		// reachable[i] marks a body position the repetition can stand at.
		reachable := make([]bool, len(src)+1)
		reachable[at+1] = true
		var ends []int
		for i := at + 1; i < len(src); i++ {
			if !reachable[i] {
				continue
			}
			switch src[i] {
			case '"':
				ends = append(ends, i+1)
			case '\\':
				reachable[i+1] = true
				if i+1 < len(src) && src[i+1] == '"' {
					reachable[i+2] = true
				}
			default:
				reachable[i+1] = true
			}
		}
		slices.Reverse(ends)
		return ends
	case '\'':
		// Lines of non-quote text, with no blank line inside.
		i := at + 1
		for {
			for i < len(src) && src[i] != '\'' && src[i] != '\n' {
				i++
			}
			if i >= len(src) {
				return nil
			}
			if src[i] == '\'' {
				return []int{i + 1}
			}
			if i+1 < len(src) && src[i+1] == '\'' {
				return []int{i + 2}
			}
			if i+1 >= len(src) || src[i+1] == '\n' {
				return nil
			}
			i++
		}
	case '(':
		for i := at + 1; i < len(src); i++ {
			switch src[i] {
			case ')':
				return []int{i + 1}
			case '(':
				return nil
			}
		}
	}
	return nil
}
