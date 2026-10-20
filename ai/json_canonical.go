package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// canonicalJavaScriptJSON retains ordinary key insertion order, but resolves
// duplicates and index keys like JSON.parse/JSON.stringify. D253 preserves
// native integer precision. Never decode objects through an unordered Go map.
func canonicalJavaScriptJSON(encoded []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	return canonicalJSONValue(decoder)
}

func canonicalJSONValue(decoder *json.Decoder) ([]byte, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch value := token.(type) {
	case json.Delim:
		var out bytes.Buffer
		out.WriteByte(byte(value))
		switch value {
		case '{':
			keys := []string{}
			fields := map[string][]byte{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				key := keyToken.(string)
				field, err := canonicalJSONValue(decoder)
				if err != nil {
					return nil, err
				}
				if _, exists := fields[key]; !exists {
					keys = appendJSObjectKey(keys, key)
				}
				fields[key] = field
			}
			for i, key := range keys {
				if i > 0 {
					out.WriteByte(',')
				}
				out.Write(canonicalJSONString(key))
				out.WriteByte(':')
				out.Write(fields[key])
			}
			out.WriteByte('}')
		case '[':
			for i := 0; decoder.More(); i++ {
				if i > 0 {
					out.WriteByte(',')
				}
				element, err := canonicalJSONValue(decoder)
				if err != nil {
					return nil, err
				}
				out.Write(element)
			}
			out.WriteByte(']')
		default:
			return nil, fmt.Errorf("unexpected JSON delimiter %q", value)
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	case string:
		return canonicalJSONString(value), nil
	case json.Number:
		return javascriptJSONNumber(string(value)), nil
	case bool:
		if value {
			return []byte("true"), nil
		}
		return []byte("false"), nil
	case nil:
		return []byte("null"), nil
	default:
		return nil, fmt.Errorf("unexpected JSON token %v", token)
	}
}

func canonicalJSONString(value string) []byte {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	// Strings cannot fail encoding. Use the base encoder to avoid recursive
	// calls through MarshalJSON during canonicalization.
	_ = encoder.Encode(value)
	encoded := out.Bytes()
	return literalJSONLineSeparators(encoded[:len(encoded)-1])
}
