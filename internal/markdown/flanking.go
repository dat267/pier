package markdown

import (
	"unicode"
	"unicode/utf8"
)

// Emphasis delimiter flanking, ported from CommonMark's rules as Marked applies
// them together with marked-cjk-friendly (upstream v1.4.0 b2d0da6fe, #10154):
// a run may open when it is left-flanking and close when it is right-flanking,
// and CJK characters count as punctuation so an emphasis marker next to CJK
// punctuation still pairs.

// isDelimiterWhitespace follows JavaScript's \s, which includes a byte-order
// mark but excludes NEXT LINE (U+0085), unlike unicode.IsSpace.
func isDelimiterWhitespace(r rune) bool {
	return r == '\ufeff' || r != '\u0085' && unicode.IsSpace(r)
}

// isDelimiterPunctuation reports whether r is punctuation or a symbol for
// flanking. CJK characters count too, which is what makes `**这是测试。**后面`
// emphasis instead of literal text.
func isDelimiterPunctuation(r rune) bool {
	return unicode.IsPunct(r) || unicode.IsSymbol(r) || isCJKRune(r)
}

// delimiterFlanking classifies a delimiter run by the characters around it:
// left reports whether the run can open, right whether it can close.
func delimiterFlanking(prev rune, hasPrev bool, next rune, hasNext bool, marker byte) (left bool, right bool) {
	prevSpace := !hasPrev || isDelimiterWhitespace(prev)
	prevPunct := hasPrev && isDelimiterPunctuation(prev) && !(marker == '*' && prev == '~')
	nextSpace := !hasNext || isDelimiterWhitespace(next)
	nextPunct := hasNext && isDelimiterPunctuation(next) && !(marker == '*' && next == '~')

	left = !nextSpace && (!nextPunct || prevSpace || prevPunct)
	right = !prevSpace && (!prevPunct || nextSpace || nextPunct)
	return left, right
}

// runeBefore decodes the rune ending just before index i.
func runeBefore(src string, i int) (rune, bool) {
	if i <= 0 || i > len(src) {
		return 0, false
	}
	r, _ := utf8.DecodeLastRuneInString(src[:i])
	if r == utf8.RuneError {
		return utf8.RuneError, true
	}
	return r, true
}

// runeAt decodes the rune starting at index i.
func runeAt(src string, i int) (rune, bool) {
	if i < 0 || i >= len(src) {
		return 0, false
	}
	r, _ := utf8.DecodeRuneInString(src[i:])
	return r, true
}
