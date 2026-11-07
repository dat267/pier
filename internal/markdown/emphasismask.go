package markdown

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// maskEmphasisSource follows Marked 18 Lexer.inlineTokens' anyPunctuation and
// blockSkip masks. Byte lengths are preserved so delimiter offsets still index
// the original UTF-8 source, rather than JavaScript's UTF-16 source.
//
// It copies only when it masks: a span with nothing to mask returns src
// itself, and a pass that masks builds its result once. The former
// []byte/string round trips were half of this function's profile on the
// streaming path (BenchmarkMarkdownStreamAppend, BenchmarkInlineIsolated).
func maskEmphasisSource(src string) string {
	return maskBlockSkip(maskEscapedPunctuation(src))
}

// maskEscapedPunctuation replaces each backslash escape of punctuation or a
// symbol (the backslash and the escaped character's bytes) with '+'.
func maskEscapedPunctuation(src string) string {
	var out strings.Builder
	last := 0
	for i := 0; i+1 < len(src); i++ {
		if src[i] != '\\' {
			continue
		}
		next, size := utf8.DecodeRuneInString(src[i+1:])
		if !unicode.IsPunct(next) && !unicode.IsSymbol(next) {
			continue
		}
		if out.Len() == 0 {
			out.Grow(len(src))
		}
		out.WriteString(src[last:i])
		end := i + 1 + size
		for range end - i {
			out.WriteByte('+')
		}
		last = end
		i = end - 1
	}
	if last == 0 {
		return src
	}
	out.WriteString(src[last:])
	return out.String()
}

// maskBlockSkip masks code spans, autolink-shaped `<...>` runs and inline
// links as '[' 'a'... ']' so emphasis delimiters inside them are skipped. It
// reads source (the escape-masked text) while writing a separate result, so a
// mask never feeds back into the scan.
func maskBlockSkip(source string) string {
	var out strings.Builder
	last := 0
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
		if end <= i {
			continue
		}
		if out.Len() == 0 {
			out.Grow(len(source))
		}
		out.WriteString(source[last:i])
		if end-i > 1 {
			out.WriteByte('[')
			for range end - i - 2 {
				out.WriteByte('a')
			}
		}
		out.WriteByte(']')
		last = end
		i = end - 1
	}
	if last == 0 {
		return source
	}
	out.WriteString(source[last:])
	return out.String()
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
