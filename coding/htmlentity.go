package coding

import (
	"strings"

	"github.com/dat267/pier/ai"
)

// Port of utils/html.ts: HTML entity decoding for the ANSI-to-HTML exporter.

// HtmlDecodedEntity is one decoded entity.
type HtmlDecodedEntity struct {
	Text string
	// Length counts consumed UTF-8 bytes for slicing the Go input string.
	Length int
}

// DecodeHtmlEntity decodes a named or numeric entity body (without & and ;).
// ok is false for unknown or out-of-range entities.
func DecodeHtmlEntity(entity string) (string, bool) {
	switch entity {
	case "amp":
		return "&", true
	case "lt":
		return "<", true
	case "gt":
		return ">", true
	case "quot":
		return "\"", true
	case "apos":
		return "'", true
	}

	if strings.HasPrefix(entity, "#x") || strings.HasPrefix(entity, "#X") {
		return decodeHtmlCodePoint(entity[2:], 16)
	}
	if strings.HasPrefix(entity, "#") {
		return decodeHtmlCodePoint(entity[1:], 10)
	}
	return "", false
}

func decodeHtmlCodePoint(digits string, base int) (string, bool) {
	codePoint, ok := jsParseInt(digits, base)
	if !ok || codePoint < 0 || codePoint > 0x10FFFF {
		return "", false
	}
	return string(rune(codePoint)), true
}

// jsParseInt parses the leading integer of text in base like Number.parseInt:
// leading whitespace and a sign are accepted, base 16 also accepts a 0x prefix,
// and parsing stops at the last valid digit (so "12abc" is 12). A value outside
// the Unicode range is saturated, which is enough for the caller's range check.
func jsParseInt(text string, base int) (int64, bool) {
	text = ai.JSTrimLeft(text)
	negative := false
	if text != "" && (text[0] == '+' || text[0] == '-') {
		negative = text[0] == '-'
		text = text[1:]
	}
	if base == 16 && len(text) >= 2 && text[0] == '0' && (text[1] == 'x' || text[1] == 'X') {
		text = text[2:]
	}
	const saturated = 0x110000
	value := int64(0)
	digits := 0
	for index := 0; index < len(text); index++ {
		digit := jsDigitValue(text[index])
		if digit < 0 || digit >= base {
			break
		}
		digits++
		if value < saturated {
			value = value*int64(base) + int64(digit)
		}
	}
	if digits == 0 {
		return 0, false
	}
	if negative {
		return -value, true
	}
	return value, true
}

// jsDigitValue returns the numeric value of an ASCII alphanumeric digit, or -1.
func jsDigitValue(char byte) int {
	switch {
	case char >= '0' && char <= '9':
		return int(char - '0')
	case char >= 'a' && char <= 'z':
		return int(char-'a') + 10
	case char >= 'A' && char <= 'Z':
		return int(char-'A') + 10
	}
	return -1
}

// DecodeHtmlEntityAt decodes the entity that starts at index in html (the
// caller has seen the '&'). ok is false when no well-formed entity ends within
// sixteen characters.
func DecodeHtmlEntityAt(html string, index int) (HtmlDecodedEntity, bool) {
	if index < 0 || index >= len(html) || html[index] != '&' {
		return HtmlDecodedEntity{}, false
	}
	rest := html[index+1:]
	semicolon := strings.Index(rest, ";")
	// The reference allows the semicolon at most sixteen code units after the
	// "&" (semicolonIndex - index > 16 is rejected).
	if semicolon == -1 || ai.JSLength(rest[:semicolon]) >= 16 {
		return HtmlDecodedEntity{}, false
	}
	text, ok := DecodeHtmlEntity(rest[:semicolon])
	if !ok {
		return HtmlDecodedEntity{}, false
	}
	return HtmlDecodedEntity{Text: text, Length: semicolon + 2}, true
}
