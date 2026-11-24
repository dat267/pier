package ai

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/dat267/pier/internal/jsnum"
)

// QuoteJSON is JSON.stringify(value) for a string, through the same canonical
// encoder as MarshalJSON (U+2028 and U+2029 stay raw, as Node leaves them).
func QuoteJSON(value string) string {
	return string(appendCanonicalJSONString(nil, value))
}

// MarshalJSON encodes v the way JavaScript's JSON.stringify does: no HTML
// escaping of <, >, and &. encoding/json escapes those by default, which
// would break byte parity with pi's transcripts and wire payloads.
func MarshalJSON(v any) ([]byte, error) {
	// Scalars and raw JSON take the canonical encoder directly; the general
	// path below encodes through the standard library and canonicalizes its
	// text. Nested message marshalers call this for every scalar field, which
	// pays the encoder/canonical pair each time (M143).
	switch value := v.(type) {
	case nil:
		return []byte("null"), nil
	case string:
		return appendCanonicalJSONString(nil, value), nil
	case bool:
		if value {
			return []byte("true"), nil
		}
		return []byte("false"), nil
	case int:
		return strconv.AppendInt(nil, int64(value), 10), nil
	case int64:
		return strconv.AppendInt(nil, value, 10), nil
	case int32:
		return strconv.AppendInt(nil, int64(value), 10), nil
	case int16:
		return strconv.AppendInt(nil, int64(value), 10), nil
	case int8:
		return strconv.AppendInt(nil, int64(value), 10), nil
	case uint:
		return strconv.AppendUint(nil, uint64(value), 10), nil
	case uint64:
		return strconv.AppendUint(nil, value, 10), nil
	case uint32:
		return strconv.AppendUint(nil, uint64(value), 10), nil
	case uint16:
		return strconv.AppendUint(nil, uint64(value), 10), nil
	case uint8:
		return strconv.AppendUint(nil, uint64(value), 10), nil
	case json.RawMessage:
		if value == nil {
			return []byte("null"), nil
		}
		if !json.Valid(value) {
			// Keep the encoder's diagnostic for malformed raw JSON; the
			// canonical scanner intentionally ignores trailing text.
			return marshalJSONGeneral(v)
		}
		return canonicalJavaScriptJSON(value)
	}
	return marshalJSONGeneral(v)
}

// marshalJSONGeneral encodes a value through encoding/json and canonicalizes
// the bytes, including its diagnostics for invalid input.
func marshalJSONGeneral(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// Encode appends a newline; strip it.
	out := buf.Bytes()
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return canonicalJavaScriptJSON(out)
}

// MarshalIndentJSON encodes v the way JavaScript's JSON.stringify(v, null, 2)
// does: two-space indentation and no HTML escaping.
func MarshalIndentJSON(v any) ([]byte, error) {
	encoded, err := MarshalJSON(v)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, encoded, "", "  "); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// RawMessage/json.Number can retain floating-point spellings that a JavaScript
// value cannot: -0, 1.0 and padded exponents. The canonical value writer uses
// this helper for number tokens. D253 keeps integral tokens exact for the
// port's typed integer fields.
func javascriptJSONNumber(token string) []byte {
	if token != "-0" && !strings.ContainsAny(token, ".eE") {
		return []byte(token)
	}
	number, _ := strconv.ParseFloat(token, 64)
	if math.IsInf(number, 0) {
		return []byte("null")
	}
	return []byte(JSNumberToString(number))
}

// encoding/json escapes U+2028/U+2029 even with HTML escaping disabled.
// JSON.stringify writes them literally. Skip escaped backslash pairs so a
// string containing the six literal characters \u2028 stays unchanged.
func literalJSONLineSeparators(encoded []byte) []byte {
	write := 0
	for read := 0; read < len(encoded); read++ {
		if encoded[read] == '\\' && read+1 < len(encoded) {
			if read+5 < len(encoded) && encoded[read+1] == 'u' &&
				encoded[read+2] == '2' && encoded[read+3] == '0' && encoded[read+4] == '2' &&
				(encoded[read+5] == '8' || encoded[read+5] == '9') {
				encoded[write], encoded[write+1], encoded[write+2] = 0xe2, 0x80, 0xa8+(encoded[read+5]-'8')
				write += 3
				read += 5
				continue
			}
			encoded[write], encoded[write+1] = encoded[read], encoded[read+1]
			write += 2
			read++
			continue
		}
		encoded[write] = encoded[read]
		write++
	}
	return encoded[:write]
}

// appendJSObjectKey records a newly added key in Object.keys order: canonical
// uint32 indices below 2^32-1 lead in numeric order; other keys keep insertion
// order. "01", "-0" and "4294967295" are ordinary string keys.
func appendJSObjectKey(keys []string, key string) []string {
	index, indexed := jsnum.ArrayIndex(key)
	if !indexed {
		return append(keys, key)
	}
	position := 0
	for position < len(keys) {
		other, ok := jsnum.ArrayIndex(keys[position])
		if !ok || other > index {
			break
		}
		position++
	}
	keys = append(keys, "")
	copy(keys[position+1:], keys[position:])
	keys[position] = key
	return keys
}
