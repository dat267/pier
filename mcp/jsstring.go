package mcp

import (
	"golang.org/x/text/encoding/unicode"
)

// JavaScript string semantics used by the MCP transports. Length, slice and
// trim come from ai/jstext.go, which the transports already import.

// decodeUTF8 matches Buffer.toString("utf8"), preserving a BOM and replacing
// each malformed subsequence rather than merging adjacent malformed bytes.
func decodeUTF8(data []byte) string {
	decoded, _ := unicode.UTF8.NewDecoder().Bytes(data)
	return string(decoded)
}
