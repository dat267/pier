package ai

import (
	"bytes"
	"unicode/utf8"
)

// canonicalJSONIsNormalized reports whether encoded is already exactly what
// the canonical parser would emit: no insignificant whitespace, canonical
// string escapes and number text, index keys first in ascending order, no
// duplicate keys, and valid UTF-8. It is a conservative filter; any doubt
// returns false so the parser keeps its diagnostics and normalization.
func canonicalJSONIsNormalized(encoded []byte) bool {
	parser := normalizedParser{data: encoded}
	// The canonical parser counts the root value as depth 1.
	if !parser.value(1) {
		return false
	}
	return parser.pos == len(encoded)
}

type normalizedParser struct {
	data []byte
	pos  int
}

func (p *normalizedParser) value(depth int) bool {
	if depth > maxJSONNestingDepth || p.pos >= len(p.data) {
		return false
	}
	switch c := p.data[p.pos]; c {
	case '{':
		return p.object(depth)
	case '[':
		return p.array(depth)
	case '"':
		return p.string()
	case 't':
		return p.literal("true")
	case 'f':
		return p.literal("false")
	case 'n':
		return p.literal("null")
	default:
		return p.number()
	}
}

func (p *normalizedParser) literal(literal string) bool {
	if !bytes.HasPrefix(p.data[p.pos:], []byte(literal)) {
		return false
	}
	p.pos += len(literal)
	return true
}

func (p *normalizedParser) array(depth int) bool {
	p.pos++
	if p.pos < len(p.data) && p.data[p.pos] == ']' {
		p.pos++
		return true
	}
	for {
		if !p.value(depth+1) || p.pos >= len(p.data) {
			return false
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return true
		default:
			return false
		}
	}
}

func (p *normalizedParser) object(depth int) bool {
	p.pos++
	if p.pos < len(p.data) && p.data[p.pos] == '}' {
		p.pos++
		return true
	}
	var keys [16][]byte
	keyCount := 0
	seenStringKey := false
	var lastIndex uint64
	for {
		if p.pos >= len(p.data) || p.data[p.pos] != '"' {
			return false
		}
		start := p.pos
		if !p.string() {
			return false
		}
		rawKey := p.data[start+1 : p.pos-1]
		if keyCount >= len(keys) {
			// Wide objects fall back rather than making the duplicate scan
			// quadratic on hostile input.
			return false
		}
		for i := 0; i < keyCount; i++ {
			if bytes.Equal(keys[i], rawKey) {
				return false
			}
		}
		keys[keyCount] = rawKey
		keyCount++
		if index, ok := normalizedArrayIndexKey(rawKey); ok {
			if seenStringKey || index <= lastIndex {
				return false
			}
			lastIndex = index
		} else {
			seenStringKey = true
		}
		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			return false
		}
		p.pos++
		if !p.value(depth+1) || p.pos >= len(p.data) {
			return false
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return true
		default:
			return false
		}
	}
}

func normalizedArrayIndexKey(key []byte) (uint64, bool) {
	if len(key) == 0 || (len(key) > 1 && key[0] == '0') || len(key) > 10 {
		return 0, false
	}
	var index uint64
	for _, c := range key {
		if c < '0' || c > '9' {
			return 0, false
		}
		index = index*10 + uint64(c-'0')
	}
	return index, index < 1<<32-1
}

func (p *normalizedParser) string() bool {
	if p.pos >= len(p.data) || p.data[p.pos] != '"' {
		return false
	}
	p.pos++
	for p.pos < len(p.data) {
		switch c := p.data[p.pos]; {
		case c == '"':
			p.pos++
			return true
		case c == '\\':
			p.pos++
			if p.pos >= len(p.data) {
				return false
			}
			switch p.data[p.pos] {
			case '"', '\\', 'b', 'f', 'n', 'r', 't':
				p.pos++
			case 'u':
				// appendCanonicalJSONString only emits \u00xx for the
				// remaining C0 controls, lower-case hex.
				if p.pos+4 >= len(p.data) || p.data[p.pos+1] != '0' || p.data[p.pos+2] != '0' {
					return false
				}
				high, okHigh := lowerHexValue(p.data[p.pos+3])
				low, okLow := lowerHexValue(p.data[p.pos+4])
				value := high<<4 | low
				if !okHigh || !okLow || value >= 0x20 || value == '\b' || value == '\t' ||
					value == '\n' || value == '\f' || value == '\r' {
					return false
				}
				p.pos += 5
			default:
				return false
			}
		case c < 0x20:
			return false
		case c < utf8.RuneSelf:
			p.pos++
		default:
			r, size := utf8.DecodeRune(p.data[p.pos:])
			if r == utf8.RuneError && size == 1 {
				return false
			}
			p.pos += size
		}
	}
	return false
}

func lowerHexValue(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	}
	return 0, false
}

func (p *normalizedParser) number() bool {
	start := p.pos
	if p.pos < len(p.data) && p.data[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(p.data) {
		return false
	}
	switch {
	case p.data[p.pos] == '0':
		p.pos++
	case p.data[p.pos] >= '1' && p.data[p.pos] <= '9':
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.pos++
		}
	default:
		return false
	}
	hasFractionOrExponent := false
	if p.pos < len(p.data) && p.data[p.pos] == '.' {
		hasFractionOrExponent = true
		p.pos++
		if p.pos >= len(p.data) || p.data[p.pos] < '0' || p.data[p.pos] > '9' {
			return false
		}
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.pos++
		}
	}
	if p.pos < len(p.data) && (p.data[p.pos] == 'e' || p.data[p.pos] == 'E') {
		hasFractionOrExponent = true
		p.pos++
		if p.pos < len(p.data) && (p.data[p.pos] == '+' || p.data[p.pos] == '-') {
			p.pos++
		}
		if p.pos >= len(p.data) || p.data[p.pos] < '0' || p.data[p.pos] > '9' {
			return false
		}
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.pos++
		}
	}
	token := p.data[start:p.pos]
	if !hasFractionOrExponent {
		return !bytes.Equal(token, []byte("-0"))
	}
	return bytes.Equal(javascriptJSONNumber(string(token)), token)
}
