package mcp

import (
	"strings"
	"unicode/utf16"

	"golang.org/x/text/encoding/unicode"
)

// JavaScript string semantics used by the MCP transports.
//
// The canonical implementation lives in ai/jstext.go, but the MCP packages are
// standalone (they must not depend on the provider layer), so the few helpers
// the transports need are repeated here with the same semantics.

// decodeUTF8 matches Buffer.toString("utf8"), preserving a BOM and replacing
// each malformed subsequence rather than merging adjacent malformed bytes.
func decodeUTF8(data []byte) string {
	decoded, _ := unicode.UTF8.NewDecoder().Bytes(data)
	return string(decoded)
}

// jsSpace is the String.prototype.trim whitespace class: Unicode space
// separators plus U+FEFF, excluding U+0085 (which Go's strings.TrimSpace
// removes).
func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

func jsTrim(s string) string { return strings.TrimFunc(s, jsSpace) }

// jsLength counts UTF-16 code units, matching JavaScript's string.length.
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

// jsSlice slices by UTF-16 code units, matching String.prototype.slice.
func jsSlice(s string, start, end int) string {
	units := utf16.Encode([]rune(s))
	length := len(units)
	if start < 0 {
		start = max(length+start, 0)
	} else if start > length {
		start = length
	}
	if end < 0 {
		end = max(length+end, 0)
	} else if end > length {
		end = length
	}
	if start >= end {
		return ""
	}
	return string(utf16.Decode(units[start:end]))
}
