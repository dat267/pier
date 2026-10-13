package tui

import (
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// jsLength counts UTF-16 code units, matching JavaScript's string.length.
// Upstream measures paste size and similar thresholds with `.length`, where an
// astral character counts as two.
func jsLength(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// jsTrim implements String.prototype.trim over the JS whitespace set that
// isWhitespaceRune encodes (it includes U+FEFF and excludes U+0085, unlike
// strings.TrimSpace).
func jsTrim(s string) string { return strings.TrimFunc(s, isWhitespaceRune) }

// jsTrimLeft implements String.prototype.trimStart.
func jsTrimLeft(s string) string { return strings.TrimLeftFunc(s, isWhitespaceRune) }

// jsLower lowercases like String.prototype.toLowerCase: full Unicode case
// mapping with the Greek final-sigma and U+0130 special cases that Go's
// strings.ToLower (simple mapping) omits.
func jsLower(s string) string { return cases.Lower(language.Und).String(s) }

// byteOffsetForUTF16Index returns the byte offset of a UTF-16 code-unit index.
func byteOffsetForUTF16Index(text string, utf16Index int) int {
	if utf16Index <= 0 {
		return 0
	}
	units := 0
	for index, r := range text {
		if units >= utf16Index {
			return index
		}
		if r > 0xFFFF {
			units += 2
		} else {
			units++
		}
	}
	return len(text)
}

// utf16IndexForByteOffset returns the UTF-16 code-unit index of a byte offset.
func utf16IndexForByteOffset(text string, byteOffset int) int {
	if byteOffset <= 0 {
		return 0
	}
	units := 0
	for index, r := range text {
		if index >= byteOffset {
			break
		}
		if r > 0xFFFF {
			units += 2
		} else {
			units++
		}
	}
	return units
}
