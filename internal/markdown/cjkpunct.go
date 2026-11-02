package markdown

import (
	"unicode"
	"unicode/utf8"
)

// Bare autolinks stop at CJK punctuation (pi v1.5.0 utils.ts
// cjkPunctuationRegex, applied by MarkdownTokenizer.url). The rule is a CJK
// character directly followed by punctuation, or one of the fullwidth marks
// that terminate a URL in CJK prose.

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

// cjkAutolinkCut returns the byte offset in raw of the first position the
// upstream rule truncates a bare autolink at, or -1 when it runs to the end.
func cjkAutolinkCut(raw string) int {
	offset := 0
	for offset < len(raw) {
		r, size := utf8.DecodeRuneInString(raw[offset:])
		if isFullwidthCJKPunctuation(r) {
			return offset
		}
		if isCJKScriptRune(r) {
			if next, _ := utf8.DecodeRuneInString(raw[offset+size:]); unicode.IsPunct(next) {
				return offset
			}
		}
		offset += size
	}
	return -1
}
