package tui

import (
	"math"
	"strconv"
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

// jsNumberString formats a float like JavaScript's String(number), the
// interpolation `${value}` performs: fixed notation from 1e-6 up to 1e21,
// exponent notation (without zero padding) outside it. It mirrors
// ai.JSNumberToString, which tui does not import.
func jsNumberString(f float64) string {
	switch {
	case f == 0:
		return "0"
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	if abs := math.Abs(f); abs >= 1e-6 && abs < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	mantissa, exponent, _ := strings.Cut(strconv.FormatFloat(f, 'e', -1, 64), "e")
	sign := ""
	if exponent != "" && (exponent[0] == '+' || exponent[0] == '-') {
		sign, exponent = exponent[:1], exponent[1:]
	}
	if exponent = strings.TrimLeft(exponent, "0"); exponent == "" {
		exponent = "0"
	}
	return mantissa + "e" + sign + exponent
}
