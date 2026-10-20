package highlight

import (
	"regexp/syntax"
	"strings"
	"unicode"
)

// This file computes two things from a matcher's joined alternation:
//
//   - the set of bytes that can begin a non-empty match, so a scan can skip
//     bytes that cannot (Go's regexp finds no common prefix for a mode's rules
//     and otherwise runs the NFA at every byte);
//   - whether the pattern begins with a context-sensitive zero-width assertion
//     (`\b`, `\B`, `^`, `\A`). Slicing the input moves the regexp's notion of
//     the text start, so a match landing exactly on the slice boundary must be
//     re-checked against the real preceding byte.
//
// The byte set is a safe over-approximation: it must never exclude a byte that
// can begin a match. A near-full set disables the skip.

// ruleFirstByteSet returns the bytes that can begin a non-empty match of one
// rule. usable is false when the rule can match empty, in which case per-byte
// dispatch cannot be used. caseInsensitive must match the language so a class
// like [A-Z_] includes its lowercase bytes.
func ruleFirstByteSet(expr string, caseInsensitive bool) (string, bool) {
	parsed := parsePatternFlags(expr, caseInsensitive)
	if parsed == nil || minMatchLen(parsed) == 0 {
		return "", false
	}
	var set [256]bool
	addFirstBytes(&set, parsed)
	var b strings.Builder
	for i := 0; i < 256; i++ {
		if set[i] {
			b.WriteByte(byte(i))
		}
	}
	return b.String(), true
}

func parsePattern(expr string) *syntax.Regexp {
	return parsePatternFlags(expr, false)
}

func parsePatternFlags(expr string, caseInsensitive bool) *syntax.Regexp {
	flags := syntax.Perl
	if caseInsensitive {
		flags |= syntax.FoldCase
	}
	parsed, err := syntax.Parse(expr, flags)
	if err != nil {
		return nil
	}
	return parsed.Simplify()
}

func minMatchLen(re *syntax.Regexp) int {
	switch re.Op {
	case syntax.OpLiteral:
		return len(re.Rune)
	case syntax.OpCharClass, syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return 1
	case syntax.OpCapture:
		return minMatchLen(re.Sub[0])
	case syntax.OpConcat:
		total := 0
		for _, sub := range re.Sub {
			total += minMatchLen(sub)
		}
		return total
	case syntax.OpAlternate:
		min := -1
		for _, sub := range re.Sub {
			v := minMatchLen(sub)
			if min < 0 || v < min {
				min = v
			}
		}
		if min < 0 {
			return 0
		}
		return min
	case syntax.OpPlus:
		return minMatchLen(re.Sub[0])
	case syntax.OpRepeat:
		if re.Min == 0 {
			return 0
		}
		return re.Min * minMatchLen(re.Sub[0])
	default:
		return 0
	}
}

// leadingContextSensitive reports whether a match can evaluate `\b`, `\B`, `^`
// or `\A` before consuming any byte, where a slice boundary would change the
// answer.
func leadingContextSensitive(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpBeginLine, syntax.OpBeginText, syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return true
	case syntax.OpCapture:
		return leadingContextSensitive(re.Sub[0])
	case syntax.OpConcat:
		for _, sub := range re.Sub {
			if leadingContextSensitive(sub) {
				return true
			}
			if minMatchLen(sub) > 0 {
				return false
			}
		}
		return false
	case syntax.OpAlternate:
		for _, sub := range re.Sub {
			if leadingContextSensitive(sub) {
				return true
			}
		}
		return false
	case syntax.OpPlus, syntax.OpStar, syntax.OpQuest, syntax.OpRepeat:
		return leadingContextSensitive(re.Sub[0])
	default:
		return false
	}
}

func addFirstBytes(set *[256]bool, re *syntax.Regexp) {
	switch re.Op {
	case syntax.OpLiteral:
		if len(re.Rune) > 0 {
			addRuneFirstByte(set, re.Rune[0])
			if re.Flags&syntax.FoldCase != 0 {
				addRuneFirstByte(set, unicode.ToUpper(re.Rune[0]))
				addRuneFirstByte(set, unicode.ToLower(re.Rune[0]))
			}
		}
	case syntax.OpCharClass:
		for i := 0; i+1 < len(re.Rune); i += 2 {
			addRuneRangeFirstBytes(set, re.Rune[i], re.Rune[i+1])
		}
	case syntax.OpAnyChar:
		for i := range set {
			set[i] = true
		}
	case syntax.OpAnyCharNotNL:
		for i := range set {
			set[i] = true
		}
		set['\n'] = false
	case syntax.OpCapture, syntax.OpPlus, syntax.OpStar, syntax.OpQuest, syntax.OpRepeat:
		addFirstBytes(set, re.Sub[0])
	case syntax.OpConcat:
		for _, sub := range re.Sub {
			addFirstBytes(set, sub)
			if minMatchLen(sub) > 0 {
				break
			}
		}
	case syntax.OpAlternate:
		for _, sub := range re.Sub {
			addFirstBytes(set, sub)
		}
	}
}

func addRuneFirstByte(set *[256]bool, r rune) {
	switch {
	case r < 0x80:
		set[byte(r)] = true
	case r < 0x800:
		set[byte(0xC0|r>>6)] = true
	case r < 0x10000:
		set[byte(0xE0|r>>12)] = true
	default:
		set[byte(0xF0|r>>18)] = true
	}
}

func addRuneRangeFirstBytes(set *[256]bool, lo, hi rune) {
	if lo < 0x80 {
		end := hi
		if end > 0x7F {
			end = 0x7F
		}
		for r := lo; r <= end; r++ {
			set[byte(r)] = true
		}
	}
	if hi >= 0x80 {
		for b := 0xC2; b <= 0xF4; b++ {
			set[byte(b)] = true
		}
	}
}
