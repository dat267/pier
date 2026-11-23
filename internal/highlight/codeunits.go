package highlight

import (
	"strings"
	"unicode/utf8"
)

// highlight.js matches JavaScript strings, whose regex engine (without /u)
// advances by UTF-16 code units. Pier keeps offsets in UTF-8 bytes, so astral
// characters are represented internally as their two surrogate units, each
// mapped to a distinct rune in Unicode plane 15. BMP characters, invalid bytes
// and every line terminator keep their identity, so ASCII classes, anchors and
// case folding behave as before. The mapping is injective over input that
// stays out of that plane-15 window: a literal private-use rune in
// U+F0000..U+FDFFF would read back as an astral character (or U+FFFD), which no
// real source file or model reply contains.
const codeUnitBase = 0xF0000

// encodeCodeUnits maps astral runes to their UTF-16 surrogate units.
func encodeCodeUnits(s string) (string, bool) {
	if !containsAstral(s) {
		return s, false
	}
	var b strings.Builder
	b.Grow(len(s) + len(s)/2)
	for i := 0; i < len(s); {
		r, width := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && width == 1 {
			b.WriteByte(s[i])
			i++
			continue
		}
		if r > 0xFFFF {
			value := r - 0x10000
			b.WriteRune(codeUnitBase + 0xD800 + value>>10)
			b.WriteRune(codeUnitBase + 0xDC00 + value&0x3FF)
			i += width
			continue
		}
		b.WriteString(s[i : i+width])
		i += width
	}
	return b.String(), true
}

func encodeCodeUnitsString(s string) string {
	encoded, _ := encodeCodeUnits(s)
	return encoded
}

func containsAstral(s string) bool {
	// An astral rune is a four-byte sequence, and its lead byte (0xF0-0xF4)
	// is never a continuation byte, so only those bytes need decoding: a
	// rune-by-rune scan reaches every one of them. ASCII is skipped a word at
	// a time; this runs over every highlighted input.
	for i := 0; i < len(s); {
		if i+8 <= len(s) && asciiWord(s[i:i+8]) {
			i += 8
			continue
		}
		if s[i] >= 0xF0 {
			if r, _ := utf8.DecodeRuneInString(s[i:]); r > 0xFFFF {
				return true
			}
		}
		i++
	}
	return false
}

// asciiWord reports whether the eight bytes of w are all ASCII.
func asciiWord(w string) bool {
	return (uint64(w[0])|uint64(w[1])<<8|uint64(w[2])<<16|uint64(w[3])<<24|
		uint64(w[4])<<32|uint64(w[5])<<40|uint64(w[6])<<48|uint64(w[7])<<56)&0x8080808080808080 == 0
}

// decodeCodeUnits reverses encodeCodeUnits. A lone mapped half is a split
// surrogate pair, which JavaScript serializes as U+FFFD.
func decodeCodeUnits(s string) string {
	if !containsCodeUnitRange(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, width := utf8.DecodeRuneInString(s[i:])
		unit := r - codeUnitBase
		if r > 0xFFFF && unit >= 0xD800 && unit <= 0xDFFF {
			next, nextWidth := utf8.DecodeRuneInString(s[i+width:])
			nextUnit := next - codeUnitBase
			if unit <= 0xDBFF && next > 0xFFFF && nextUnit >= 0xDC00 && nextUnit <= 0xDFFF {
				b.WriteRune(0x10000 + (unit-0xD800)<<10 + (nextUnit - 0xDC00))
				i += width + nextWidth
				continue
			}
			b.WriteRune(utf8.RuneError)
			i += width
			continue
		}
		b.WriteString(s[i : i+width])
		i += width
	}
	return b.String()
}

// jsLineTerminatorAt reports a JavaScript line terminator at a byte offset.
func jsLineTerminatorAt(s string, i int) bool {
	if i >= len(s) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return r == '\n' || r == '\r' || r == '\u2028' || r == '\u2029'
}

func containsCodeUnitRange(s string) bool {
	for i := 0; i < len(s); {
		r, width := utf8.DecodeRuneInString(s[i:])
		if r > 0xFFFF {
			unit := r - codeUnitBase
			if unit >= 0xD800 && unit <= 0xDFFF {
				return true
			}
		}
		i += width
	}
	return false
}

// decodeCodeUnitNodes restores astral characters in a highlight result. A
// regex can split a pair across adjacent unstyled text nodes; JavaScript's
// HTML concatenates those halves back into one character, while markup between
// them (a styled node) leaves two lone surrogates that serialize as U+FFFD.
func decodeCodeUnitNodes(nodes []*Node) []*Node {
	merged := nodes[:0]
	for _, node := range nodes {
		if node.Text != "" && node.Children == nil && len(merged) > 0 {
			last := merged[len(merged)-1]
			if last.Text != "" && last.Children == nil {
				last.Text += node.Text
				continue
			}
		}
		merged = append(merged, node)
	}
	for _, node := range merged {
		if node.Text != "" {
			node.Text = decodeCodeUnits(node.Text)
		}
		node.Children = decodeCodeUnitNodes(node.Children)
	}
	return merged
}
