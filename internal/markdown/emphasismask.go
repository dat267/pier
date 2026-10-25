package markdown

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// maskEmphasisSource follows Marked 18 Lexer.inlineTokens' anyPunctuation and
// blockSkip masks. Byte lengths are preserved so delimiter offsets still index
// the original UTF-8 source, rather than JavaScript's UTF-16 source.
func maskEmphasisSource(src string) string {
	masked := []byte(src)
	for i := 0; i+1 < len(src); i++ {
		if src[i] != '\\' {
			continue
		}
		next, size := utf8.DecodeRuneInString(src[i+1:])
		if unicode.IsPunct(next) || unicode.IsSymbol(next) {
			for end := i + 1 + size; i < end; i++ {
				masked[i] = '+'
			}
			i--
		}
	}
	source := string(masked)
	for i := 0; i < len(source); i++ {
		end := -1
		switch source[i] {
		case '`':
			if i == 0 || source[i-1] != '`' {
				end = emphasisCodeMaskEnd(source, i)
			}
		case '<':
			if i+1 < len(source) && source[i+1] != ' ' {
				if offset := strings.IndexAny(source[i+1:], "<>"); offset >= 0 && source[i+1+offset] == '>' {
					end = i + offset + 2
				}
			}
		case '[':
			end = emphasisLinkMaskEnd(source, i)
		}
		if end > i {
			masked[i] = '['
			for j := i + 1; j < end-1; j++ {
				masked[j] = 'a'
			}
			masked[end-1] = ']'
			i = end - 1
		}
	}
	return string(masked)
}

// Marked's blockSkip code alternative requires non-backtick content followed
// immediately by a backtick run of the opening length.
func emphasisCodeMaskEnd(src string, start int) int {
	content := start
	for content < len(src) && src[content] == '`' {
		content++
	}
	offset := strings.IndexByte(src[content:], '`')
	if offset <= 0 {
		return -1
	}
	close := content + offset
	end := close
	for end < len(src) && src[end] == '`' {
		end++
	}
	if end-close != content-start {
		return -1
	}
	return end
}

func emphasisLinkMaskEnd(src string, start int) int {
	i := start + 1
	for i < len(src) && src[i] != ']' {
		if src[i] == '[' {
			return -1
		}
		if src[i] == '`' {
			i = emphasisCodeMaskEnd(src, i)
			if i < 0 {
				return -1
			}
		} else {
			i++
		}
	}
	if i+1 >= len(src) || src[i+1] != '(' {
		return -1
	}
	depth := 1
	for i += 2; i < len(src); i++ {
		switch src[i] {
		case '\\':
			i++
		case '(':
			depth++
			if depth > 2 {
				return -1
			}
		case ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}
