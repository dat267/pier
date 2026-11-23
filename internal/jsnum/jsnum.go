// Package jsnum ports the JavaScript numeric coercions that a JSON field's
// declared type does not enforce upstream, and the array-index rule that
// orders an object's keys. ai, coding and tui share this one copy.
package jsnum

import (
	"cmp"
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
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

// ToInt converts a JavaScript number to int, truncating toward zero. A value
// outside int's range saturates at its bound, because the port cannot
// represent that number in its result type (D313, D336).
func ToInt(value float64) int {
	if value >= math.MaxInt {
		return math.MaxInt
	}
	if value <= math.MinInt {
		return math.MinInt
	}
	return int(value)
}

// ToInt64 is ToInt for int64 fields: a finite positive catalog or
// configuration value saturates instead of wrapping negative (D334, D336,
// D337).
func ToInt64(value float64) int64 {
	if value >= math.MaxInt64 {
		return math.MaxInt64
	}
	if value <= math.MinInt64 {
		return math.MinInt64
	}
	return int64(value)
}

// ArrayIndex reports whether key is a JavaScript array index ("0", or digits
// with no leading zero, below 2^32-1) and its value. "01", "-0" and
// "4294967295" are ordinary string keys. It validates the digits itself:
// strconv.ParseUint allocates an error for every ordinary key, which
// dominated JSON canonicalization allocations (M143).
func ArrayIndex(key string) (uint32, bool) {
	if key == "" || (len(key) > 1 && key[0] == '0') || len(key) > 10 {
		return 0, false
	}
	var index uint64
	for i := range len(key) {
		if key[i] < '0' || key[i] > '9' {
			return 0, false
		}
		index = index*10 + uint64(key[i]-'0')
	}
	if index >= 1<<32-1 {
		return 0, false
	}
	return uint32(index), true
}

// ObjectKeyOrder returns keys in the order Object.keys and JSON.stringify
// enumerate them: array-index keys first, ascending, then the rest in their
// own order.
func ObjectKeyOrder(keys []string) []string {
	type indexedKey struct {
		name  string
		index uint32
	}
	indices := make([]indexedKey, 0, len(keys))
	ordered := make([]string, 0, len(keys))
	var ordinary []string
	for _, key := range keys {
		if index, ok := ArrayIndex(key); ok {
			indices = append(indices, indexedKey{name: key, index: index})
		} else {
			ordinary = append(ordinary, key)
		}
	}
	slices.SortStableFunc(indices, func(a, b indexedKey) int { return cmp.Compare(a.index, b.index) })
	for _, entry := range indices {
		ordered = append(ordered, entry.name)
	}
	return append(ordered, ordinary...)
}

// MaxTimeoutMS is the largest delay setTimeout honours, 2^31-1 ms.
const MaxTimeoutMS int64 = 1<<31 - 1

// TimeoutDuration is the delay setTimeout(fn, ms) waits: Node and Bun run a
// delay below 1 ms or above MaxTimeoutMS after 1 ms.
func TimeoutDuration(ms int64) time.Duration {
	if ms < 1 || ms > MaxTimeoutMS {
		ms = 1
	}
	return time.Duration(ms) * time.Millisecond
}
