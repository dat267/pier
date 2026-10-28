package highlight

import (
	"strconv"
	"strings"
)

// JavaScript regexes without /u accept several Annex B escapes and quantifier
// forms that Go's parser rejects or interprets differently:
//
//   - `\uXXXX` is a UTF-16 code unit escape; a value in D800-DFFF becomes the
//     mapped unit rune used for astral matching. `\u` without four hex digits
//     is an identity escape for "u".
//   - `\xHH` is a two-digit escape; otherwise \x is an identity escape for "x".
//   - a brace group is a quantifier only where a quantifier is allowed and its
//     bounds are valid; otherwise `{` and `}` are literal.
//   - an empty class `[]` never matches and `[^]` matches any code unit.
//   - a `-` next to a class escape is literal rather than a range operator.
func normalizeJSEscapes(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	for i := 0; i < len(src); {
		if src[i] != '\\' || i+1 >= len(src) {
			b.WriteByte(src[i])
			i++
			continue
		}
		switch src[i+1] {
		case 'u':
			if value, ok := hexDigits(src[i+2:], 4); ok {
				if value >= 0xD800 && value <= 0xDFFF {
					b.WriteString(`\x{`)
					b.WriteString(strconv.FormatInt(int64(codeUnitBase+value), 16))
					b.WriteString(`}`)
				} else {
					b.WriteString(`\x{`)
					b.WriteString(padHex(value, 4))
					b.WriteString(`}`)
				}
				i += 6
				continue
			}
			b.WriteByte('u')
			i += 2
			continue
		case 'x':
			if value, ok := hexDigits(src[i+2:], 2); ok {
				b.WriteString(`\x{`)
				b.WriteString(padHex(value, 2))
				b.WriteString(`}`)
				i += 4
				continue
			}
			b.WriteByte('x')
			i += 2
			continue
		}
		b.WriteString(src[i : i+2])
		i += 2
	}
	return b.String()
}

func hexDigits(s string, count int) (int, bool) {
	if len(s) < count {
		return 0, false
	}
	value := 0
	for i := 0; i < count; i++ {
		digit, ok := hexValue(s[i])
		if !ok {
			return 0, false
		}
		value = value<<4 | digit
	}
	return value, true
}

func hexValue(b byte) (int, bool) {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0'), true
	case b >= 'a' && b <= 'f':
		return int(b-'a') + 10, true
	case b >= 'A' && b <= 'F':
		return int(b-'A') + 10, true
	}
	return 0, false
}

func padHex(value, width int) string {
	out := strconv.FormatInt(int64(value), 16)
	for len(out) < width {
		out = "0" + out
	}
	return out
}

// normalizeJSBracesAndClasses escapes non-quantifier braces, rewrites empty
// classes and makes class-escape hyphens literal.
func normalizeJSBracesAndClasses(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	canRepeat := false
	for i := 0; i < len(src); {
		switch src[i] {
		case '\\':
			if i+2 < len(src) && src[i+1] == 'x' && src[i+2] == '{' {
				if close := strings.IndexByte(src[i+3:], '}'); close >= 0 {
					end := i + 3 + close + 1
					if _, ok := hexDigits(src[i+3:i+3+close], close); ok {
						b.WriteString(src[i:end])
						i = end
						canRepeat = true
						continue
					}
				}
			}
			if i+1 < len(src) {
				b.WriteString(src[i : i+2])
				i += 2
			} else {
				b.WriteByte(src[i])
				i++
			}
			canRepeat = true
		case '[':
			end := jsClassEnd(src, i)
			if end < 0 {
				b.WriteString(src[i:])
				i = len(src)
				break
			}
			b.WriteString(normalizeJSClass(src[i : end+1]))
			i = end + 1
			canRepeat = true
		case '{':
			if canRepeat {
				if end, ok := quantifierEnd(src, i); ok {
					b.WriteString(src[i:end])
					i = end
					canRepeat = false
					continue
				}
			}
			b.WriteString(`\{`)
			i++
			canRepeat = true
		case '}':
			b.WriteString(`\}`)
			i++
			canRepeat = true
		case '^', '$', '|', '(':
			b.WriteByte(src[i])
			i++
			canRepeat = false
		case '*', '+', '?':
			b.WriteByte(src[i])
			i++
			canRepeat = false
		case ')':
			b.WriteByte(src[i])
			i++
			canRepeat = true
		default:
			b.WriteByte(src[i])
			i++
			canRepeat = true
		}
	}
	return b.String()
}

// jsClassEnd returns the index of the `]` that closes the class, following
// JavaScript's rule that it is the first unescaped `]`, even when empty.
func jsClassEnd(src string, start int) int {
	for i := start + 1; i < len(src); i++ {
		if src[i] == '\\' {
			i++
			continue
		}
		if src[i] == ']' {
			return i
		}
	}
	return -1
}

func normalizeJSClass(class string) string {
	content := class[1 : len(class)-1]
	negated := strings.HasPrefix(content, "^")
	body := strings.TrimPrefix(content, "^")
	if body == "" {
		if negated {
			return `[\x00-\x{10ffff}]`
		}
		return `[^\x00-\x{10ffff}]`
	}
	return "[" + strings.Repeat("^", boolToInt(negated)) + literalClassHyphens(body) + "]"
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func isClassEscape(b byte) bool {
	switch b {
	case 'd', 'D', 'w', 'W', 's', 'S':
		return true
	}
	return false
}

// literalClassHyphens turns `-` adjacent to a class escape into an escaped
// literal, matching JavaScript's Annex B range rule.
func literalClassHyphens(body string) string {
	var b strings.Builder
	b.Grow(len(body))
	for i := 0; i < len(body); {
		if body[i] == '\\' && i+1 < len(body) {
			b.WriteString(body[i : i+2])
			if isClassEscape(body[i+1]) && i+2 < len(body) && body[i+2] == '-' {
				b.WriteString(`\-`)
				i += 3
				continue
			}
			i += 2
			continue
		}
		if body[i] == '-' && i+2 < len(body) && body[i+1] == '\\' && isClassEscape(body[i+2]) {
			b.WriteString(`\-`)
			i++
			continue
		}
		b.WriteByte(body[i])
		i++
	}
	return b.String()
}

// quantifierEnd returns the index after a valid `{n}`, `{n,}` or `{n,m}` whose
// bounds Go can compile.
func quantifierEnd(src string, start int) (int, bool) {
	i := start + 1
	minStart := i
	for i < len(src) && src[i] >= '0' && src[i] <= '9' {
		i++
	}
	if i == minStart {
		return 0, false
	}
	min, err := strconv.Atoi(src[minStart:i])
	if err != nil || min > 1000 {
		return 0, false
	}
	if i < len(src) && src[i] == ',' {
		i++
		maxStart := i
		for i < len(src) && src[i] >= '0' && src[i] <= '9' {
			i++
		}
		if i > maxStart {
			limit, err := strconv.Atoi(src[maxStart:i])
			if err != nil || limit > 1000 || limit < min {
				return 0, false
			}
		}
	}
	if i >= len(src) || src[i] != '}' {
		return 0, false
	}
	return i + 1, true
}
