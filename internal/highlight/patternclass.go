package highlight

import (
	"regexp/syntax"
	"strconv"
	"strings"
)

// JavaScript character classes range over UTF-16 code units while the encoded
// text stores surrogate units as plane-15 runes. A positive class whose BMP
// range crosses D800-DFFF (for example Julia's [\u00A1-\uFFFF]) must therefore
// also include the mapped units; a negated class must exclude them. Classes
// that already name an astral character exclude or include its mapped units
// explicitly and need no change.
func augmentSurrogateClasses(src string) string {
	var b strings.Builder
	last := 0
	for i := 0; i < len(src); {
		if src[i] == '\\' {
			i += 2
			continue
		}
		if src[i] != '[' {
			i++
			continue
		}
		end := i + 1
		escaped := false
		for end < len(src) && (escaped || src[end] != ']') {
			escaped = src[end] == '\\' && !escaped
			end++
		}
		if end >= len(src) {
			break
		}
		if patch := mappedSurrogateRanges(src[i+1 : end]); patch != "" {
			b.WriteString(src[last : i+1])
			b.WriteString(src[i+1 : end])
			b.WriteString(patch)
			last = end
		}
		i = end + 1
	}
	if last == 0 {
		return src
	}
	b.WriteString(src[last:])
	return b.String()
}

// mappedSurrogateRanges returns the encoded ranges for the surrogate units a
// class mentions, or "" when it mentions none.
func mappedSurrogateRanges(content string) string {
	content = strings.TrimPrefix(content, "^")
	parsed, err := syntax.Parse("["+content+"]", syntax.Perl)
	if err != nil || parsed.Op != syntax.OpCharClass {
		return ""
	}
	var patch strings.Builder
	for i := 0; i+1 < len(parsed.Rune); i += 2 {
		low := max(parsed.Rune[i], 0xD800)
		high := min(parsed.Rune[i+1], 0xDFFF)
		if low > high {
			continue
		}
		writeMappedRange(&patch, low, high)
	}
	return patch.String()
}

func writeMappedRange(b *strings.Builder, low, high rune) {
	b.WriteString(`\x{`)
	b.WriteString(strconv.FormatInt(int64(codeUnitBase+low), 16))
	b.WriteString(`}`)
	if high > low {
		b.WriteString(`-\x{`)
		b.WriteString(strconv.FormatInt(int64(codeUnitBase+high), 16))
		b.WriteString(`}`)
	}
}

// jsSpaceUnits is ECMAScript WhiteSpace plus LineTerminator, one entry per
// UTF-16 code unit.
var jsSpaceUnits = []rune{
	0x0009, 0x000A, 0x000B, 0x000C, 0x000D, 0x0020, 0x00A0, 0x1680,
	0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006, 0x2007,
	0x2008, 0x2009, 0x200A, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF,
}

// expandClassComplements rewrites a character class containing \S. A positive
// class becomes the union of its other members and the non-whitespace set; a
// negated class keeps the whitespace units its other members do not reject.
func expandClassComplements(src string) string {
	if !strings.Contains(src, `\S`) {
		return src
	}
	var b strings.Builder
	last := 0
	for i := 0; i < len(src); {
		if src[i] == '\\' {
			i += 2
			continue
		}
		if src[i] != '[' {
			i++
			continue
		}
		end := i + 1
		escaped := false
		for end < len(src) && (escaped || src[end] != ']') {
			escaped = src[end] == '\\' && !escaped
			end++
		}
		if end >= len(src) {
			break
		}
		content := src[i+1 : end]
		replacement, changed := rewriteClassComplement(content)
		if changed {
			b.WriteString(src[last:i])
			b.WriteString(replacement)
			last = end + 1
		}
		i = end + 1
	}
	if last == 0 {
		return src
	}
	b.WriteString(src[last:])
	return b.String()
}

func rewriteClassComplement(content string) (string, bool) {
	negated := strings.HasPrefix(content, "^")
	body := strings.TrimPrefix(content, "^")
	var rest strings.Builder
	found := false
	for i := 0; i < len(body); {
		if body[i] == '\\' && i+1 < len(body) {
			if body[i+1] == 'S' {
				found = true
				i += 2
				continue
			}
			rest.WriteString(body[i : i+2])
			i += 2
			continue
		}
		rest.WriteByte(body[i])
		i++
	}
	if !found {
		return "", false
	}
	if !negated {
		if rest.Len() == 0 {
			return `[^` + jsSpaceRunes + `]`, true
		}
		return `(?:[` + rest.String() + `]|[^` + jsSpaceRunes + `])`, true
	}
	ranges, ok := classRanges(rest.String())
	if !ok {
		return "", false
	}
	var kept strings.Builder
	for _, unit := range jsSpaceUnits {
		if containsRune(ranges, unit) {
			continue
		}
		kept.WriteString(`\x{`)
		kept.WriteString(strconv.FormatInt(int64(unit), 16))
		kept.WriteString(`}`)
	}
	if kept.Len() == 0 {
		return `[^\x00-\x{10ffff}]`, true
	}
	return `[` + kept.String() + `]`, true
}

func classRanges(content string) ([]rune, bool) {
	if content == "" {
		return nil, true
	}
	parsed, err := syntax.Parse("["+content+"]", syntax.Perl)
	if err != nil || parsed.Op != syntax.OpCharClass {
		return nil, false
	}
	return parsed.Rune, true
}

func containsRune(ranges []rune, value rune) bool {
	for i := 0; i+1 < len(ranges); i += 2 {
		if value >= ranges[i] && value <= ranges[i+1] {
			return true
		}
	}
	return false
}
