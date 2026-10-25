package markdown

import (
	"strings"
	"unicode/utf8"
)

// Bare autolinks are the Marked 18 gfm inline `url` rule, which pi v1.4.0
// inherits from packages/tui/src/components/markdown.ts. The rule links
// `http(s)://`, `ftp://` and lowercase `www.` targets plus bare email
// addresses, then trims trailing punctuation with `_backpedal`.

// inlineState mirrors the flags the reference lexer keeps for one document.
// inLink suppresses bare autolinks inside link labels and `<a ` regions.
type inlineState struct {
	linkEmitted bool
	inLink      bool
}

// matchAutolinkPrefixForm matches the rule's `protocol://`/`www.` alternative.
func matchAutolinkPrefixForm(src string, i int) (text, dest string, length int, ok bool) {
	if i >= len(src) {
		return "", "", 0, false
	}
	prefix := matchAutolinkPrefix(src, i)
	if prefix == 0 {
		return "", "", 0, false
	}
	j := i + prefix
	host := false
	for j < len(src) {
		run := 0
		for j+run < len(src) && isURLHostChar(src[j+run]) {
			run++
		}
		if run == 0 {
			break
		}
		j += run
		if j < len(src) && src[j] == '.' {
			j++
		}
		host = true
	}
	if !host {
		return "", "", 0, false
	}
	for j < len(src) {
		r, size := utf8.DecodeRuneInString(src[j:])
		if r == '<' || isWhitespaceRune(r) {
			break
		}
		j += size
	}
	raw := backpedalURL(src[i:j])
	if raw == "" {
		return "", "", 0, false
	}
	if src[i:i+prefix] == "www." {
		return raw, "http://" + raw, len(raw), true
	}
	return raw, raw, len(raw), true
}

// matchAutolinkPrefix returns the length of a `protocol://` or `www.` prefix.
func matchAutolinkPrefix(src string, i int) int {
	rest := src[i:]
	if rest == "" {
		return 0
	}
	switch rest[0] {
	case 'w':
		// The reference rule spells this prefix in lowercase only.
		if strings.HasPrefix(rest, "www.") {
			return 4
		}
	case 'h', 'H':
		for _, scheme := range [...]string{"https://", "http://"} {
			if len(rest) >= len(scheme) && strings.EqualFold(rest[:len(scheme)], scheme) {
				return len(scheme)
			}
		}
	case 'f', 'F':
		if len(rest) >= 6 && strings.EqualFold(rest[:6], "ftp://") {
			return 6
		}
	}
	return 0
}

// matchBareEmail matches the rule's `email` alternative at src[i:].
func matchBareEmail(src string, i int) (int, bool) {
	j := i
	for j < len(src) && isEmailLocalChar(src[j]) {
		j++
	}
	if j == i || j >= len(src) || src[j] != '@' {
		return 0, false
	}
	j++
	if j >= len(src) || !isEmailDomainChar(src[j]) {
		return 0, false
	}
	for j < len(src) && isEmailDomainChar(src[j]) {
		j++
	}
	// (?:\. [a-zA-Z0-9-_]* [a-zA-Z0-9])+ greedily consumes each dotted label
	// through its last alphanumeric and must repeat at least once. Each label's
	// alphanumeric offsets are the positions its repetition may end at.
	var groups [][]int
	for end := j; end < len(src) && src[end] == '.'; {
		labelEnd := end + 1
		var alnums []int
		for labelEnd < len(src) && isEmailDomainChar(src[labelEnd]) {
			if isAlnum(src[labelEnd]) {
				alnums = append(alnums, labelEnd)
			}
			labelEnd++
		}
		if len(alnums) == 0 {
			break
		}
		groups = append(groups, alnums)
		end = alnums[len(alnums)-1] + 1
	}
	if len(groups) == 0 {
		return 0, false
	}
	// The trailing (?![-_]) lookahead backtracks the last label to an earlier
	// alphanumeric, then drops whole groups; a dropped group leaves the
	// previous group's end before the group's dot, which always passes.
	for {
		last := groups[len(groups)-1]
		for k := len(last) - 1; k >= 0; k-- {
			if last[k]+1 >= len(src) || (src[last[k]+1] != '-' && src[last[k]+1] != '_') {
				return last[k] + 1, true
			}
		}
		groups = groups[:len(groups)-1]
		if len(groups) == 0 {
			return 0, false
		}
		previous := groups[len(groups)-1]
		return previous[len(previous)-1] + 1, true
	}
}

// backpedalURL applies the reference's `_backpedal` regex to a URL match until
// it stops changing.
func backpedalURL(raw string) string {
	for {
		next := backpedalPrefix(raw)
		if next == raw {
			return raw
		}
		raw = next
	}
}

// backpedalPrefix scans the reference regex
// (?:[^?!.,:;*_'"~()&]+|\([^)]*\)|&(?![a-zA-Z0-9]+;$)|[?!.,:;*_'"~)]+(?!$))+
// from the start of s.
func backpedalPrefix(s string) string {
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case !isBackpedalStop(r):
			for i < len(s) {
				next, nextSize := utf8.DecodeRuneInString(s[i:])
				if isBackpedalStop(next) {
					break
				}
				i += nextSize
			}
		case r == '(':
			close := strings.IndexByte(s[i+1:], ')')
			if close < 0 {
				return s[:i]
			}
			i += close + 2
		case r == '&':
			if isEntityTail(s[i+1:]) {
				return s[:i]
			}
			i += size
		case isBackpedalTrailing(r):
			j := i
			for j < len(s) {
				next, nextSize := utf8.DecodeRuneInString(s[j:])
				if !isBackpedalTrailing(next) {
					break
				}
				j += nextSize
			}
			if j == len(s) {
				// (?!$) makes the run backtrack to a shorter prefix that still
				// has a following character, leaving the last character unmatched.
				if j-i < 2 {
					return s[:i]
				}
				j--
			}
			i = j
		default:
			return s[:i]
		}
	}
	return s
}

// isBackpedalStop reports membership of the reference's negated class
// [^?!.,:;*_'"~()&].
func isBackpedalStop(r rune) bool {
	switch r {
	case '?', '!', '.', ',', ':', ';', '*', '_', '\'', '"', '~', '(', ')', '&':
		return true
	}
	return false
}

// isBackpedalTrailing reports membership of the reference's trailing class
// [?!.,:;*_'"~)].
func isBackpedalTrailing(r rune) bool {
	switch r {
	case '?', '!', '.', ',', ':', ';', '*', '_', '\'', '"', '~', ')':
		return true
	}
	return false
}

// isEntityTail matches [a-zA-Z0-9]+;$ against the remainder of the string.
func isEntityTail(rest string) bool {
	if len(rest) < 2 || rest[len(rest)-1] != ';' {
		return false
	}
	body := rest[:len(rest)-1]
	for i := 0; i < len(body); i++ {
		if !isAlnum(body[i]) {
			return false
		}
	}
	return true
}

func isURLHostChar(c byte) bool {
	return isAlnum(c) || c == '-'
}

func isEmailLocalChar(c byte) bool {
	return isAlnum(c) || c == '.' || c == '_' || c == '+' || c == '-'
}

func isEmailDomainChar(c byte) bool {
	return isAlnum(c) || c == '-' || c == '_'
}

// canStartBareAutolink is the cheap first-byte filter for the inline loop: a
// protocol/www prefix or an email local part.
func canStartBareAutolink(c byte) bool {
	return isAlnum(c) || c == '.' || c == '_' || c == '+' || c == '-'
}

// containsBareAutolink reports whether src has a bare autolink, without the
// cost of matching at every offset.
func containsBareAutolink(src string) bool {
	for i := 0; i < len(src); i++ {
		if prefix := matchAutolinkPrefix(src, i); prefix > 0 {
			if i+prefix < len(src) && isURLHostChar(src[i+prefix]) {
				return true
			}
		}
		if isEmailLocalChar(src[i]) && (i == 0 || !isEmailLocalChar(src[i-1])) {
			if _, ok := matchBareEmail(src, i); ok {
				return true
			}
		}
	}
	return false
}
