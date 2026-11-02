package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf16"
	"unicode/utf8"
)

// canonicalJavaScriptJSON retains ordinary key insertion order, but resolves
// duplicates and index keys like JSON.parse/JSON.stringify. D253 preserves
// native integer precision. Never decode objects through an unordered Go map.
//
// The scanner parses one value and appends canonical bytes to a single output
// buffer, mirroring the reference's byte output (including JavaScript string
// escaping and number formatting) without constructing a decoder token per
// element. M143: the decoder-based version allocated a token, a buffer and an
// encoder per string, costing 456k allocations on a 754 KB conversation.
func canonicalJavaScriptJSON(encoded []byte) ([]byte, error) {
	parser := canonicalParser{data: encoded}
	parser.skipSpace()
	out := make([]byte, 0, len(encoded))
	out, err := parser.value(out)
	if err != nil {
		// Invalid input only: let the decoder produce the standard diagnostic
		// wording that callers report (D269); valid JSON never takes this path.
		return canonicalJSONDecoderFallback(encoded)
	}
	return out, nil
}

type canonicalParser struct {
	data  []byte
	pos   int
	depth int
}

// maxJSONNestingDepth mirrors the decoder's limit: accepting deeper input would
// recurse until the stack fails on hostile input instead of reporting an error.
const maxJSONNestingDepth = 10000

func (p *canonicalParser) skipSpace() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *canonicalParser) value(out []byte) ([]byte, error) {
	if p.pos >= len(p.data) {
		return nil, io.EOF
	}
	switch p.data[p.pos] {
	case '{':
		return p.object(out)
	case '[':
		return p.array(out)
	case '"':
		value, err := p.parseString()
		if err != nil {
			return nil, err
		}
		return appendCanonicalJSONString(out, value), nil
	case 't':
		return p.literal(out, trueLiteral)
	case 'f':
		return p.literal(out, falseLiteral)
	case 'n':
		return p.literal(out, nullLiteral)
	default:
		return p.number(out)
	}
}

var (
	trueLiteral  = []byte("true")
	falseLiteral = []byte("false")
	nullLiteral  = []byte("null")
)

func (p *canonicalParser) literal(out []byte, literal []byte) ([]byte, error) {
	if len(p.data)-p.pos < len(literal) {
		return nil, io.ErrUnexpectedEOF
	}
	if !bytes.Equal(p.data[p.pos:p.pos+len(literal)], literal) {
		return nil, fmt.Errorf("canonical JSON: invalid literal")
	}
	p.pos += len(literal)
	return append(out, literal...), nil
}

func (p *canonicalParser) number(out []byte) ([]byte, error) {
	start := p.pos
	if p.data[p.pos] == '-' {
		p.pos++
		if p.pos >= len(p.data) {
			return nil, io.ErrUnexpectedEOF
		}
	}
	switch {
	case p.data[p.pos] == '0':
		p.pos++
	case p.data[p.pos] >= '1' && p.data[p.pos] <= '9':
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.pos++
		}
	default:
		return nil, fmt.Errorf("canonical JSON: invalid number")
	}
	if p.pos < len(p.data) && p.data[p.pos] == '.' {
		p.pos++
		if p.pos >= len(p.data) || p.data[p.pos] < '0' || p.data[p.pos] > '9' {
			return nil, io.ErrUnexpectedEOF
		}
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.pos++
		}
	}
	if p.pos < len(p.data) && (p.data[p.pos] == 'e' || p.data[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.data) && (p.data[p.pos] == '+' || p.data[p.pos] == '-') {
			p.pos++
		}
		if p.pos >= len(p.data) || p.data[p.pos] < '0' || p.data[p.pos] > '9' {
			return nil, io.ErrUnexpectedEOF
		}
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.pos++
		}
	}
	return append(out, javascriptJSONNumber(string(p.data[start:p.pos]))...), nil
}

func (p *canonicalParser) array(out []byte) ([]byte, error) {
	p.depth++
	if p.depth > maxJSONNestingDepth {
		return nil, fmt.Errorf("canonical JSON: exceeded max depth")
	}
	defer func() { p.depth-- }()
	p.pos++
	out = append(out, '[')
	p.skipSpace()
	if p.pos < len(p.data) && p.data[p.pos] == ']' {
		p.pos++
		return append(out, ']'), nil
	}
	for index := 0; ; index++ {
		p.skipSpace()
		var err error
		out, err = p.value(out)
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if p.pos >= len(p.data) {
			return nil, io.ErrUnexpectedEOF
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
			out = append(out, ',')
		case ']':
			p.pos++
			return append(out, ']'), nil
		default:
			return nil, fmt.Errorf("canonical JSON: invalid array separator")
		}
	}
}

func (p *canonicalParser) object(out []byte) ([]byte, error) {
	p.depth++
	if p.depth > maxJSONNestingDepth {
		return nil, fmt.Errorf("canonical JSON: exceeded max depth")
	}
	defer func() { p.depth-- }()
	p.pos++
	p.skipSpace()
	if p.pos < len(p.data) && p.data[p.pos] == '}' {
		p.pos++
		return append(out, '{', '}'), nil
	}
	var keys []string
	fields := map[string][]byte{}
	for {
		p.skipSpace()
		if p.pos >= len(p.data) || p.data[p.pos] != '"' {
			return nil, fmt.Errorf("canonical JSON: invalid object key")
		}
		key, err := p.parseString()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			return nil, fmt.Errorf("canonical JSON: missing object colon")
		}
		p.pos++
		p.skipSpace()
		field, err := p.value(nil)
		if err != nil {
			return nil, err
		}
		if _, exists := fields[key]; !exists {
			keys = appendJSObjectKey(keys, key)
		}
		fields[key] = field
		p.skipSpace()
		if p.pos >= len(p.data) {
			return nil, io.ErrUnexpectedEOF
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			out = append(out, '{')
			for index, key := range keys {
				if index > 0 {
					out = append(out, ',')
				}
				out = appendCanonicalJSONString(out, key)
				out = append(out, ':')
				out = append(out, fields[key]...)
			}
			return append(out, '}'), nil
		default:
			return nil, fmt.Errorf("canonical JSON: invalid object separator")
		}
	}
}

// parseString decodes a JSON string; the fast path returns a substring when it
// has no escapes. Invalid UTF-8 and lone surrogate escapes become U+FFFD, as
// the reference decoder does.
func (p *canonicalParser) parseString() (string, error) {
	if p.pos >= len(p.data) || p.data[p.pos] != '"' {
		return "", fmt.Errorf("canonical JSON: expected string")
	}
	p.pos++
	start := p.pos
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		switch {
		case c == '"':
			value := string(p.data[start:p.pos])
			p.pos++
			return value, nil
		case c == '\\':
			return p.parseEscapedString(start)
		case c < 0x20:
			return "", fmt.Errorf("canonical JSON: invalid control character")
		case c < utf8.RuneSelf:
			p.pos++
		default:
			r, size := utf8.DecodeRune(p.data[p.pos:])
			if r == utf8.RuneError && size == 1 {
				// Invalid UTF-8 needs the replacement-aware slow path.
				return p.parseEscapedString(start)
			}
			p.pos += size
		}
	}
	return "", io.ErrUnexpectedEOF
}

func (p *canonicalParser) parseEscapedString(start int) (string, error) {
	var builder []byte
	builder = append(builder, p.data[start:p.pos]...)
	for {
		if p.pos >= len(p.data) {
			return "", io.ErrUnexpectedEOF
		}
		switch c := p.data[p.pos]; {
		case c == '"':
			p.pos++
			return string(builder), nil
		case c == '\\':
			p.pos++
			if p.pos >= len(p.data) {
				return "", io.ErrUnexpectedEOF
			}
			switch escape := p.data[p.pos]; escape {
			case '"', '\\', '/':
				builder = append(builder, escape)
				p.pos++
			case 'b':
				builder = append(builder, '\b')
				p.pos++
			case 'f':
				builder = append(builder, '\f')
				p.pos++
			case 'n':
				builder = append(builder, '\n')
				p.pos++
			case 'r':
				builder = append(builder, '\r')
				p.pos++
			case 't':
				builder = append(builder, '\t')
				p.pos++
			case 'u':
				p.pos++
				value, ok := p.hex4()
				if !ok {
					return "", fmt.Errorf("canonical JSON: invalid unicode escape")
				}
				if !utf16.IsSurrogate(value) {
					builder = utf8.AppendRune(builder, value)
					continue
				}
				high := value
				if p.pos+1 < len(p.data) && p.data[p.pos] == '\\' && p.data[p.pos+1] == 'u' {
					save := p.pos
					p.pos += 2
					low, ok := p.hex4()
					if !ok {
						return "", fmt.Errorf("canonical JSON: invalid unicode escape")
					}
					if low >= 0xDC00 && low <= 0xDFFF {
						builder = utf8.AppendRune(builder, utf16.DecodeRune(high, low))
						continue
					}
					// Not a low surrogate: emit U+FFFD for the high one and
					// reprocess this escape from its backslash.
					p.pos = save
				}
				builder = utf8.AppendRune(builder, utf8.RuneError)
			default:
				return "", fmt.Errorf("canonical JSON: invalid escape sequence")
			}
		case c < 0x20:
			return "", fmt.Errorf("canonical JSON: invalid control character")
		default:
			r, size := utf8.DecodeRune(p.data[p.pos:])
			if r == utf8.RuneError && size == 1 {
				builder = utf8.AppendRune(builder, utf8.RuneError)
			} else {
				builder = append(builder, p.data[p.pos:p.pos+size]...)
			}
			p.pos += size
		}
	}
}

func (p *canonicalParser) hex4() (rune, bool) {
	if p.pos+4 > len(p.data) {
		return 0, false
	}
	var value rune
	for _, c := range p.data[p.pos : p.pos+4] {
		value <<= 4
		switch {
		case c >= '0' && c <= '9':
			value |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			value |= rune(c-'a') + 10
		case c >= 'A' && c <= 'F':
			value |= rune(c-'A') + 10
		default:
			return 0, false
		}
	}
	p.pos += 4
	return value, true
}

// appendCanonicalJSONString writes a string the way JSON.stringify does:
// control characters use the short escapes or lowercase \u00XX, U+2028 and
// U+2029 stay literal, HTML characters are not escaped, and invalid UTF-8
// becomes U+FFFD.
func appendCanonicalJSONString(out []byte, value string) []byte {
	const hex = "0123456789abcdef"
	out = append(out, '"')
	for index := 0; index < len(value); {
		c := value[index]
		switch c {
		case '"':
			out = append(out, '\\', '"')
			index++
		case '\\':
			out = append(out, '\\', '\\')
			index++
		case '\b':
			out = append(out, '\\', 'b')
			index++
		case '\f':
			out = append(out, '\\', 'f')
			index++
		case '\n':
			out = append(out, '\\', 'n')
			index++
		case '\r':
			out = append(out, '\\', 'r')
			index++
		case '\t':
			out = append(out, '\\', 't')
			index++
		default:
			if c < 0x20 {
				out = append(out, '\\', 'u', '0', '0', hex[c>>4], hex[c&0x0f])
				index++
				continue
			}
			if c < utf8.RuneSelf {
				out = append(out, c)
				index++
				continue
			}
			r, size := utf8.DecodeRuneInString(value[index:])
			if r == utf8.RuneError && size == 1 {
				out = utf8.AppendRune(out, utf8.RuneError)
				index++
				continue
			}
			out = append(out, value[index:index+size]...)
			index += size
		}
	}
	return append(out, '"')
}

// canonicalJSONDecoderFallback is the decoder-based implementation used when
// the scanner rejects input. It keeps the standard library's diagnostic text,
// which callers surface for unparseable provider bodies (D269); the slow path
// only runs on error.
func canonicalJSONDecoderFallback(encoded []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	return canonicalJSONDecoderValue(decoder)
}

func canonicalJSONDecoderValue(decoder *json.Decoder) ([]byte, error) {
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
				field, err := canonicalJSONDecoderValue(decoder)
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
				element, err := canonicalJSONDecoderValue(decoder)
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
