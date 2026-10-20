package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Port of utils/json-parse.ts.
//
// D-row D3: upstream's partial fallback uses the npm `partial-json` package.
// The Go port implements the subset of partial-JSON semantics the streaming
// path needs (truncated objects/arrays/strings keep their complete prefix
// fields); verify byte-for-byte against partial-json before relying on
// exotic inputs.

const validJSONEscapes = `"\/bfnrt"`

// RepairJSON repairs malformed JSON string literals by escaping raw control
// characters inside strings and doubling backslashes before invalid escape
// characters (port of repairJson, operating on UTF-16 code units like the
// JS original).
func RepairJSON(jsonText string) string {
	units := utf16.Encode([]rune(jsonText))
	var repaired []uint16
	inString := false

	// JS string indexing is by UTF-16 unit.
	at := func(i int) (uint16, bool) {
		if i < 0 || i >= len(units) {
			return 0, false
		}
		return units[i], true
	}

	for index := 0; index < len(units); index++ {
		char := units[index]

		if !inString {
			repaired = append(repaired, char)
			if char == '"' {
				inString = true
			}
			continue
		}

		if char == '"' {
			repaired = append(repaired, char)
			inString = false
			continue
		}

		if char == '\\' {
			nextChar, ok := at(index + 1)
			if !ok {
				repaired = append(repaired, '\\', '\\')
				continue
			}

			if nextChar == 'u' {
				// Slice of 4 units after \u.
				valid := index+6 <= len(units)
				if valid {
					for _, d := range units[index+2 : index+6] {
						hexOK := (d >= '0' && d <= '9') || (d >= 'a' && d <= 'f') || (d >= 'A' && d <= 'F')
						if !hexOK {
							valid = false
							break
						}
					}
				}
				if valid {
					repaired = append(repaired, units[index:index+6]...)
					index += 5
					continue
				}
			}

			// nextChar is a single code unit for the escape set.
			nc := string(utf16.Decode([]uint16{nextChar}))
			if len(nc) == 1 && strings.ContainsRune(validJSONEscapes, rune(nc[0])) {
				repaired = append(repaired, '\\', nextChar)
				index++
				continue
			}

			repaired = append(repaired, '\\', '\\')
			continue
		}

		if char < 0x20 {
			// Control character: escape it.
			enc, _ := MarshalJSON(string(utf16.Decode([]uint16{char})))
			// enc is a quoted JSON string like "\u0001" or "\b".
			s := string(enc)
			inner := s[1 : len(s)-1]
			for _, r := range inner {
				repaired = append(repaired, uint16(r))
			}
		} else {
			repaired = append(repaired, char)
		}
	}

	return string(utf16.Decode(repaired))
}

// ParseJSONWithRepair parses JSON, retrying once with RepairJSON when the
// repair changes the input (port of parseJsonWithRepair).
func ParseJSONWithRepair(jsonText string, v any) error {
	err := json.Unmarshal([]byte(jsonText), v)
	if err == nil {
		return nil
	}
	repaired := RepairJSON(jsonText)
	if repaired != jsonText {
		return json.Unmarshal([]byte(repaired), v)
	}
	return err
}

// ParseToolCallArguments parses the model's complete arguments text of a
// finished tool call (port of parseToolCallArguments). Unlike
// parseStreamingJSONInto, it never keeps a partially parsed prefix: once the
// call is finished, text that is not a valid JSON object means the model sent
// malformed arguments. The returned arguments are then `{}` and the second
// result describes the problem, including the text the model sent, so tool
// validation can report it back to the model.
//
// D221: the JS `JSON.parse`/repair error text is not reproduced; the Go
// decoder's message is used instead. The shape (message, blank line, "Received
// arguments:", raw text) and the `{}` fallback match upstream.
func ParseToolCallArguments(jsonText string) (json.RawMessage, string) {
	if JSTrim(jsonText) == "" {
		return json.RawMessage(`{}`), ""
	}
	value, err := decodeOrderedJSON([]byte(jsonText))
	if err != nil {
		if repaired := RepairJSON(jsonText); repaired != jsonText {
			if repairedValue, repairedErr := decodeOrderedJSON([]byte(repaired)); repairedErr == nil {
				value, err = repairedValue, nil
			}
		}
	}
	if err != nil {
		return json.RawMessage(`{}`), fmt.Sprintf("%s\n\nReceived arguments:\n%s", err.Error(), jsonText)
	}
	// Upstream's typeof/Array.isArray guard: null, arrays and scalars are not an
	// argument object.
	if value.Object == nil {
		return json.RawMessage(`{}`), "Tool arguments must be a JSON object\n\nReceived arguments:\n" + jsonText
	}
	return encodeSchemaValue(value), ""
}

// decodeOrderedJSON parses one JSON value keeping object member order.
// Upstream parses tool arguments with JSON.parse and re-stringifies the object,
// so the model's key order survives; a map[string]any would sort it (D6's
// ordered representation is reused here). Trailing data is rejected like
// JSON.parse.
func decodeOrderedJSON(data []byte) (*schemaValue, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	value, err := decodeOrderedToken(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("unexpected data after the JSON value")
	}
	return value, nil
}

func decodeOrderedToken(dec *json.Decoder) (*schemaValue, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case nil:
		return &schemaValue{Null: true}, nil
	case bool:
		return &schemaValue{Bool: t, IsBool: true}, nil
	case json.Number:
		number, err := t.Float64()
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return nil, err
		}
		return &schemaValue{Number: number, IsNumber: true}, nil
	case string:
		return &schemaValue{String: t, IsString: true}, nil
	case json.Delim:
		switch t {
		case '{':
			object := &schemaObject{values: map[string]*schemaValue{}}
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, fmt.Errorf("object key must be a string")
				}
				value, err := decodeOrderedToken(dec)
				if err != nil {
					return nil, err
				}
				object.set(key, value)
			}
			if _, err := dec.Token(); err != nil { // closing }
				return nil, err
			}
			return &schemaValue{Object: object}, nil
		case '[':
			array := []*schemaValue{}
			for dec.More() {
				value, err := decodeOrderedToken(dec)
				if err != nil {
					return nil, err
				}
				array = append(array, value)
			}
			if _, err := dec.Token(); err != nil { // closing ]
				return nil, err
			}
			return &schemaValue{IsArray: true, Array: array}, nil
		}
	}
	return nil, fmt.Errorf("unexpected JSON token")
}

// parseOrderedJSONObject re-encodes a JSON object preserving key order
// (JSON.parse then JSON.stringify semantics). ok is false when data is not a
// JSON object.
func parseOrderedJSONObject(data []byte) (json.RawMessage, bool) {
	value, err := decodeOrderedJSON(data)
	if err != nil || value.Object == nil {
		return nil, false
	}
	return encodeSchemaValue(value), true
}

// parseStreamingJSONObject attempts to parse potentially incomplete JSON during
// streaming, preserving object key order (upstream parseStreamingJson). An
// empty or unparseable input, or a non-object result, yields {}.
func parseStreamingJSONObject(partialJSON string) json.RawMessage {
	if JSTrim(partialJSON) == "" {
		return json.RawMessage(`{}`)
	}
	if encoded, ok := parseOrderedJSONObject([]byte(partialJSON)); ok {
		return encoded
	}
	repaired := RepairJSON(partialJSON)
	if repaired != partialJSON {
		if encoded, ok := parseOrderedJSONObject([]byte(repaired)); ok {
			return encoded
		}
	}
	if value, err := partialParse(partialJSON); err == nil && value.Object != nil {
		return encodeSchemaValue(value)
	}
	if repaired != partialJSON {
		if value, err := partialParse(repaired); err == nil && value.Object != nil {
			return encodeSchemaValue(value)
		}
	}
	return json.RawMessage(`{}`)
}

// parseStreamingJSONInto is the map-shaped form used by tests; production
// callers re-encode the ordered object directly.
func parseStreamingJSONInto(partialJSON string, v any) {
	_ = json.Unmarshal(parseStreamingJSONObject(partialJSON), v)
}

// partialParse parses truncated JSON, keeping complete prefix fields
// (truncated strings/objects/arrays). It mirrors the npm partial-json
// package's default behavior for the streaming cases pi relies on, and keeps
// object key order in the same representation the ordered JSON decoder uses.
func partialParse(text string) (*schemaValue, error) {
	p := &partialParser{units: utf16.Encode([]rune(text))}
	v, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	// Trailing whitespace only is fine; anything else is left partial.
	p.skipWhitespace()
	return v, nil
}

type partialParser struct {
	units []uint16
	pos   int
}

func (p *partialParser) peek() (uint16, bool) {
	if p.pos >= len(p.units) {
		return 0, false
	}
	return p.units[p.pos], true
}

func (p *partialParser) skipWhitespace() {
	for p.pos < len(p.units) {
		u := p.units[p.pos]
		if u == ' ' || u == '\t' || u == '\n' || u == '\r' {
			p.pos++
		} else {
			return
		}
	}
}

func (p *partialParser) parseValue() (*schemaValue, error) {
	p.skipWhitespace()
	u, ok := p.peek()
	if !ok {
		return nil, fmt.Errorf("partial json: unexpected end of input")
	}
	switch u {
	case '{':
		return p.parseObject()
	case '[':
		return p.parseArray()
	case '"':
		s, err := p.parsePartialString()
		if err != nil {
			return nil, err
		}
		return &schemaValue{String: s, IsString: true}, nil
	default:
		return p.parseLiteral()
	}
}

func (p *partialParser) parseObject() (*schemaValue, error) {
	p.pos++ // '{'
	out := &schemaObject{values: map[string]*schemaValue{}}
	result := func() *schemaValue { return &schemaValue{Object: out} }
	for {
		p.skipWhitespace()
		u, ok := p.peek()
		if !ok {
			return result(), nil // truncated after '{' or ','
		}
		if u == '}' {
			p.pos++
			return result(), nil
		}
		if u == ',' {
			p.pos++
			continue
		}
		if u != '"' {
			return result(), nil // partial — stop with what we have
		}
		key, ok := p.tryParseFullString()
		if !ok {
			return result(), nil
		}
		p.skipWhitespace()
		u, ok = p.peek()
		if !ok {
			return result(), nil
		}
		if u != ':' {
			return result(), nil
		}
		p.pos++
		value, err := p.parseValue()
		if err != nil {
			return result(), nil
		}
		out.set(key, value)
		p.skipWhitespace()
		if u2, ok := p.peek(); ok && u2 == ',' {
			p.pos++
			continue
		}
		if u2, ok := p.peek(); !ok || u2 != '}' {
			return result(), nil // truncated value list
		}
	}
}

func (p *partialParser) parseArray() (*schemaValue, error) {
	p.pos++ // '['
	out := []*schemaValue{}
	result := func() *schemaValue { return &schemaValue{IsArray: true, Array: out} }
	for {
		p.skipWhitespace()
		u, ok := p.peek()
		if !ok {
			return result(), nil
		}
		if u == ']' {
			p.pos++
			return result(), nil
		}
		if u == ',' {
			p.pos++
			continue
		}
		value, err := p.parseValue()
		if err != nil {
			return result(), nil
		}
		out = append(out, value)
		p.skipWhitespace()
		if u2, ok := p.peek(); ok && u2 == ',' {
			p.pos++
			continue
		}
		if u2, ok := p.peek(); !ok || u2 != ']' {
			return result(), nil
		}
	}
}

// parsePartialString parses a possibly-truncated JSON string; an
// unterminated string yields the complete prefix (partial-json semantics).
// parsePartialString parses a (possibly unterminated) JSON string. Escape
// sequences are decoded by the standard JSON decoder, so every escape behaves
// the same here as in a complete document (including surrogate pairs); an
// invalid or truncated escape ends the string, like partial-json.
func (p *partialParser) parsePartialString() (string, error) {
	p.pos++ // opening quote
	var builder strings.Builder
	pending := p.pos // start of the literal span not yet emitted
	for p.pos < len(p.units) {
		u := p.units[p.pos]
		if u == '"' {
			builder.WriteString(p.sliceUnits(pending, p.pos))
			p.pos++
			return builder.String(), nil
		}
		if u != '\\' {
			p.pos++
			continue
		}

		builder.WriteString(p.sliceUnits(pending, p.pos))
		// Decode the maximal well-formed run of escapes. Decoding the run as a
		// whole lets the JSON decoder combine a surrogate pair, and stops at the
		// first escape it rejects.
		runStart := p.pos
		end := runStart
		decoded := ""
		for {
			size := escapeUnitLength(p.units, end)
			if size == 0 {
				break
			}
			var out string
			if err := json.Unmarshal([]byte(`"`+p.sliceUnits(runStart, end+size)+`"`), &out); err != nil {
				break
			}
			decoded = out
			end += size
		}
		if end == runStart {
			// The first escape is invalid or truncated: stop the string here
			// (everything from the backslash on is dropped).
			return builder.String(), nil
		}
		builder.WriteString(decoded)
		p.pos = end
		pending = end
	}
	builder.WriteString(p.sliceUnits(pending, p.pos))
	return builder.String(), nil
}

// sliceUnits converts a UTF-16 code-unit span to a string (the parser indexes
// in code units, like the JS original).
func (p *partialParser) sliceUnits(from, to int) string {
	return string(utf16.Decode(p.units[from:to]))
}

// escapeUnitLength returns the code-unit length of the JSON escape starting at
// pos: 1 for \u plus its four digits, otherwise \ plus one unit. 0 means the
// input is truncated or does not start an escape.
func escapeUnitLength(units []uint16, pos int) int {
	if pos >= len(units) || units[pos] != '\\' || pos+1 >= len(units) {
		return 0
	}
	if units[pos+1] == 'u' {
		if pos+6 > len(units) {
			return 0
		}
		return 6
	}
	return 2
}

// tryParseFullString parses a complete string; ok is false when truncated.
func (p *partialParser) tryParseFullString() (string, bool) {
	s, err := p.parsePartialString()
	if err != nil {
		return "", false
	}
	// Distinguish complete from truncated: parsePartialString returns on
	// closing quote (pos past it) or end of input. Check the last consumed
	// unit was a quote by re-examining: if pos <= len(units) and the
	// preceding unit is '"' it was complete.
	if p.pos-1 < len(p.units) && p.units[p.pos-1] == '"' {
		return s, true
	}
	return "", false
}

func (p *partialParser) parseLiteral() (*schemaValue, error) {
	rest := string(utf16.Decode(p.units[p.pos:]))
	for _, lit := range []struct {
		name  string
		value *schemaValue
	}{
		{"true", &schemaValue{Bool: true, IsBool: true}},
		{"false", &schemaValue{Bool: false, IsBool: true}},
		{"null", &schemaValue{Null: true}},
	} {
		if strings.HasPrefix(rest, lit.name) {
			p.pos += len(lit.name)
			return lit.value, nil
		}
	}
	// Numbers: consume valid numeric characters.
	end := 0
	for end < len(rest) {
		c := rest[end]
		if (c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E' {
			end++
		} else {
			break
		}
	}
	if end == 0 {
		return nil, fmt.Errorf("partial json: unexpected character %q", rest[0])
	}
	numText := rest[:end]
	p.pos += end
	var f float64
	if err := json.Unmarshal([]byte(numText), &f); err != nil {
		return nil, err
	}
	return &schemaValue{Number: f, IsNumber: true}, nil
}
