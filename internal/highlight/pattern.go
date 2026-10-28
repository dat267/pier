package highlight

import (
	"regexp"
	"strings"
)

// This file bridges highlight.js's regular expressions to Go's RE2 engine.
//
// highlight.js patterns are JavaScript regex sources. Most are already valid
// RE2 (97% of the patterns the reference compiles for pi's eager languages),
// but a few constructs differ:
//
//   - `\uXXXX` and `\u{...}` are JavaScript unicode escapes; Go spells them
//     `\x{XXXX}`.
//   - `\/` is an escaped solidus. Go accepts it, so it passes through.
//   - lookahead and lookbehind have no RE2 equivalent and are handled by the
//     caller with an explicit replacement (see replacePattern).
//   - backreferences have no RE2 equivalent and are replaced by hand.
//
// normalizePattern rewrites the mechanical differences. It leaves lookaround
// and backreferences in place so a caller can detect them and substitute a
// hand-written equivalent.

// hasLookaround reports whether src contains a lookahead or lookbehind.
func hasLookaround(src string) bool {
	return strings.Contains(src, "(?=") || strings.Contains(src, "(?!") ||
		strings.Contains(src, "(?<=") || strings.Contains(src, "(?<!")
}

// hasBackreference reports whether src contains a numeric backreference.
func hasBackreference(src string) bool {
	for i := 0; i+1 < len(src); i++ {
		if src[i] != '\\' {
			continue
		}
		c := src[i+1]
		if c >= '1' && c <= '9' {
			return true
		}
		i++
	}
	return false
}

// normalizePattern rewrites the JavaScript-isms RE2 does not accept but that
// have a direct equivalent: `\uXXXX` and `\u{...}` become `\x{...}`, and the
// whitespace classes use ECMAScript's WhiteSpace + LineTerminator set rather
// than Go's ASCII-only `\s`. `[\s\S]` is the reference's any-character idiom.
func normalizePattern(src string) string {
	src = normalizeUnicodeEscapes(src)
	return normalizeWhitespaceClasses(src)
}

// jsSpaceRunes is ECMAScript WhiteSpace plus LineTerminator.
const jsSpaceRunes = `\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

func normalizeWhitespaceClasses(src string) string {
	if !strings.ContainsAny(src, `sS`) {
		return src
	}
	// `[\s\S]` is JavaScript's any-character idiom; RE2 has no class
	// subtraction for a general in-class complement.
	src = strings.ReplaceAll(src, `[\s\S]`, `[\x00-\x{10ffff}]`)
	var b strings.Builder
	b.Grow(len(src) + 32)
	inClass := false
	for i := 0; i < len(src); {
		c := src[i]
		if c == '\\' && i+1 < len(src) {
			switch src[i+1] {
			case 's':
				if inClass {
					b.WriteString(jsSpaceRunes)
				} else {
					b.WriteString("[")
					b.WriteString(jsSpaceRunes)
					b.WriteString("]")
				}
				i += 2
				continue
			case 'S':
				if inClass {
					// Outside the pinned snapshot, an in-class complement cannot be
					// expressed as a RE2 class; leave Go's ASCII meaning.
					b.WriteString(`\S`)
					i += 2
					continue
				}
				b.WriteString("[^")
				b.WriteString(jsSpaceRunes)
				b.WriteString("]")
				i += 2
				continue
			}
			// Any other escape, including an escaped backslash, is copied verbatim.
			b.WriteString(src[i : i+2])
			i += 2
			continue
		}
		switch c {
		case '[':
			if !inClass {
				inClass = true
			}
		case ']':
			inClass = false
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

func normalizeUnicodeEscapes(src string) string {
	if !strings.Contains(src, `\u`) {
		return src
	}
	var b strings.Builder
	b.Grow(len(src))
	for i := 0; i < len(src); {
		if src[i] != '\\' || i+1 >= len(src) {
			b.WriteByte(src[i])
			i++
			continue
		}
		if src[i+1] == 'u' {
			if i+2 < len(src) && src[i+2] == '{' {
				// \u{XXXX} -> \x{XXXX}
				b.WriteString(`\x`)
				i += 2
				continue
			}
			if i+5 < len(src) {
				b.WriteString(`\x{`)
				b.WriteString(src[i+2 : i+6])
				b.WriteByte('}')
				i += 6
				continue
			}
		}
		// Any other escape, including an escaped backslash, is copied verbatim
		// so `\\u` is not mistaken for a unicode escape.
		b.WriteByte(src[i])
		b.WriteByte(src[i+1])
		i += 2
	}
	return b.String()
}

// compilePattern compiles a highlight.js regex source with the given flags.
// Only the flags that affect matching (i, m, s) are meaningful to RE2; the
// stateful ones (g, y) are ignored.
func compilePattern(src string, flags string) (*patternRegexp, error) {
	var goFlags strings.Builder
	// highlight.js always compiles with the multiline flag (langRe).
	goFlags.WriteString("m")
	for _, f := range flags {
		switch f {
		case 'i':
			goFlags.WriteString("i")
		case 's':
			goFlags.WriteString("s")
		}
	}
	expression := "(?" + goFlags.String() + ")" + normalizePattern(src)
	re, err := regexp.Compile(expression)
	if err != nil {
		return nil, err
	}
	return &patternRegexp{Regexp: re, lineSensitive: strings.ContainsAny(src, "^$.")}, nil
}
