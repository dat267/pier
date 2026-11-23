// Package jsnum ports the JavaScript numeric coercions that a JSON field's
// declared type does not enforce upstream.
package jsnum

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// Whitespace reports JavaScript's whitespace set (WhiteSpace and
// LineTerminator), which `\s`, trim and Number() use: the Unicode space
// separators, U+2028, U+2029 and the byte-order mark, but not the U+0085 that
// unicode.IsSpace includes. It is the one copy ai, tui, mcp and markdown share;
// the switch measured twice as fast as unicode.IsSpace on the per-character
// wrapping path.
func Whitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00A0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// Finite mirrors the `Number.isFinite` check.
func Finite(value float64) (float64, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

// Number coerces like Number(), which expires_in uses upstream. Arrays coerce
// through their single element, as Number([5]) is 5 and Number([]) is 0.
// Number(value any) maps JSON null to 0 like Number(null). An absent field is
// undefined upstream and must be rejected by the caller before coercing.
func Number(value any) (float64, bool) {
	switch v := value.(type) {
	case nil:
		return 0, true
	case float64:
		return Finite(v)
	case json.Number:
		n, err := v.Float64()
		if err != nil {
			return 0, false
		}
		return Finite(n)
	case string:
		// Number("") is 0, Number("0x10") is 16, and Number trims JavaScript
		// whitespace (including a BOM).
		text := strings.TrimFunc(v, Whitespace)
		if text == "" {
			return 0, true
		}
		// Number() has no numeric separators, but Go's ParseFloat accepts them:
		// "1_0" is NaN upstream and 10 here.
		if strings.ContainsRune(text, '_') {
			return 0, false
		}
		if len(text) > 2 && text[0] == '0' {
			base := 0
			switch text[1] {
			case 'x', 'X':
				base = 16
			case 'b', 'B':
				base = 2
			case 'o', 'O':
				base = 8
			}
			if base != 0 {
				integer, err := strconv.ParseUint(text[2:], base, 64)
				if err != nil {
					return 0, false
				}
				return float64(integer), true
			}
		}
		number, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, false
		}
		return Finite(number)
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	case []any:
		switch len(v) {
		case 0:
			return 0, true
		case 1:
			return Number(v[0])
		}
		return 0, false
	}
	return 0, false
}
