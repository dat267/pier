package markdown

import (
	"unicode"
	"unicode/utf8"
)

// Bare autolinks stop at CJK punctuation (pi v1.5.2 (b4afdddf) utils.ts
// cjkPunctuationRegex, applied by markdown.ts MarkdownTokenizer.url). The
// regex has two alternatives: a zero-width position at a character that is
// itself punctuation and carries a CJK script extension (cjkBreakRegex), and
// the explicit fullwidth marks that terminate a URL in CJK prose. The cut
// keeps every character before that position, so the CJK letter in front of a
// full stop stays inside the link.

// isCJKScriptRune mirrors the scripts of cjkBreakRegex
// (\p{Script_Extensions=Han|Hiragana|Katakana|Hangul|Bopomofo}).
func isCJKScriptRune(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Bopomofo)
}

// isFullwidthCJKPunctuation is the regex's explicit punctuation set.
func isFullwidthCJKPunctuation(r rune) bool {
	switch r {
	case '，', '．', '：', '；', '！', '？', '（', '）', '［', '］', '｛', '｝',
		'“', '”', '‘', '’', '…', '—':
		return true
	}
	return false
}

// isCJKPunctuationScriptRune lists the punctuation whose Script_Extensions
// include a CJK script, which Go's script tables do not encode (they carry the
// Script only, and these characters are Script=Common): U+00B7, the CJK
// symbols and punctuation block, the CJK brackets, U+30FB, the vertical
// presentation marks and the halfwidth CJK punctuation. Derived from
// \p{Punctuation} ∩ cjkBreakRegex in the reference's runtime.
func isCJKPunctuationScriptRune(r rune) bool {
	switch {
	case r == 0x00B7, r == 0x16FE2:
		return true
	case r >= 0x3001 && r <= 0x3003:
		return true
	case r >= 0x3008 && r <= 0x3011:
		return true
	case r >= 0x3014 && r <= 0x301F:
		return true
	case r == 0x3030, r == 0x303D, r == 0x30A0, r == 0x30FB:
		return true
	case r == 0xFE45, r == 0xFE46:
		return true
	case r >= 0xFF61 && r <= 0xFF65:
		return true
	}
	return false
}

// cjkAutolinkCut returns the byte offset in raw of the first position the
// upstream rule truncates a bare autolink at, or -1 when it runs to the end.
func cjkAutolinkCut(raw string) int {
	offset := 0
	for offset < len(raw) {
		r, size := utf8.DecodeRuneInString(raw[offset:])
		if unicode.IsPunct(r) && (isCJKScriptRune(r) || isCJKPunctuationScriptRune(r)) {
			return offset
		}
		if isFullwidthCJKPunctuation(r) {
			return offset
		}
		offset += size
	}
	return -1
}
