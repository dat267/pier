package highlight

import (
	"regexp/syntax"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
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
	for i := range 256 {
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
		minimum := -1
		for _, sub := range re.Sub {
			v := minMatchLen(sub)
			if minimum < 0 || v < minimum {
				minimum = v
			}
		}
		if minimum < 0 {
			return 0
		}
		return minimum
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
		return slices.ContainsFunc(re.Sub, leadingContextSensitive)
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
		end := min(hi, 0x7F)
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

// rulePrefilter returns a rule's parsed form for mayMatchAt, or nil when the
// walk could check no more than the first byte, which the dispatch already
// matched: such a prefilter would only add its call to every candidate.
func rulePrefilter(expr string, caseInsensitive bool) *syntax.Regexp {
	re := parsePatternFlags(expr, caseInsensitive)
	if re == nil || prefilterDepth(re) <= 1 {
		return nil
	}
	return re
}

// prefilterDepth is the fewest characters the walk checks on any path through
// re's understood prefix (see mayMatchAt), stopping at the first node it would
// answer "may match" for.
func prefilterDepth(re *syntax.Regexp) int {
	switch re.Op {
	case syntax.OpLiteral:
		return len(re.Rune)
	case syntax.OpCharClass:
		return 1
	case syntax.OpCapture:
		return prefilterDepth(re.Sub[0])
	case syntax.OpConcat:
		total := 0
		for _, sub := range re.Sub {
			switch sub.Op {
			case syntax.OpEmptyMatch, syntax.OpBeginLine, syntax.OpEndLine, syntax.OpBeginText,
				syntax.OpEndText, syntax.OpWordBoundary, syntax.OpNoWordBoundary:
				continue
			case syntax.OpStar, syntax.OpPlus, syntax.OpQuest:
				// A run is checked, then the rest of the rule after it.
				total++
				continue
			}
			depth := prefilterDepth(sub)
			total += depth
			if depth == 0 || (sub.Op != syntax.OpLiteral && sub.Op != syntax.OpCharClass) {
				return total
			}
		}
		return total
	case syntax.OpAlternate:
		minimum := -1
		for _, sub := range re.Sub {
			if depth := prefilterDepth(sub); minimum < 0 || depth < minimum {
				minimum = depth
			}
		}
		return max(minimum, 0)
	}
	return 0
}

// prefilterBudget bounds one mayMatchAt walk; past it the walk answers "may
// match", so the check stays a few dozen steps however a rule is shaped.
const prefilterBudget = 96

// prefilterRun bounds the class runs the walk enumerates.
const prefilterRun = 64

// prefilterCont is the rest of a concatenation, as a list of nodes linked by
// index into the walk's reused slice, so a walk does not allocate.
type prefilterCont struct {
	node *syntax.Regexp
	next int32
}

// prefilterEnd is the empty list: the end of the rule.
const prefilterEnd = -1

// prefilterWalk is one mayMatchAt evaluation. conts is kept in the
// highlight's workspace and reused, so only its first growth allocates.
type prefilterWalk struct {
	s      string
	budget int
	conts  []prefilterCont
}

// mayMatchAt is a necessary condition for the rule to match at pos: false
// means it cannot. It follows literals, ASCII classes, alternations and
// bounded single-character repetitions over the input, skips zero-width
// assertions (dropping a condition only weakens the check), and answers true
// for anything else, a non-ASCII byte, or a spent budget (D522).
func mayMatchAt(re *syntax.Regexp, s string, pos int, ws *jsWorkspace) bool {
	w := prefilterWalk{s: s, budget: prefilterBudget}
	if ws != nil {
		w.conts = ws.prefilter[:0]
	}
	ok := w.seq(w.push(re, prefilterEnd), pos)
	if ws != nil {
		ws.prefilter = w.conts[:0]
	}
	return ok
}

func (w *prefilterWalk) push(node *syntax.Regexp, next int32) int32 {
	w.conts = append(w.conts, prefilterCont{node: node, next: next})
	return int32(len(w.conts) - 1)
}

// seq walks the list from pos; the empty list is the end of the rule, a match.
func (w *prefilterWalk) seq(seq int32, pos int) bool {
	s := w.s
	for seq != prefilterEnd {
		w.budget--
		if w.budget < 0 {
			return true
		}
		node, rest := w.conts[seq].node, w.conts[seq].next
		switch node.Op {
		case syntax.OpEmptyMatch, syntax.OpBeginLine, syntax.OpEndLine, syntax.OpBeginText,
			syntax.OpEndText, syntax.OpWordBoundary, syntax.OpNoWordBoundary:
			seq = rest
		case syntax.OpLiteral:
			for _, r := range node.Rune {
				if pos >= len(s) {
					return false
				}
				if r >= utf8.RuneSelf || s[pos] >= utf8.RuneSelf {
					return true
				}
				if !prefilterLiteral(node, r, s[pos]) {
					return false
				}
				pos++
			}
			seq = rest
		case syntax.OpCharClass:
			if pos >= len(s) {
				return false
			}
			if s[pos] >= utf8.RuneSelf {
				return true
			}
			if !prefilterClassHas(node.Rune, rune(s[pos])) {
				return false
			}
			pos++
			seq = rest
		case syntax.OpCapture:
			seq = w.push(node.Sub[0], rest)
		case syntax.OpConcat:
			for i := len(node.Sub) - 1; i >= 0; i-- {
				rest = w.push(node.Sub[i], rest)
			}
			seq = rest
		case syntax.OpAlternate:
			for _, alternative := range node.Sub {
				if w.seq(w.push(alternative, rest), pos) {
					return true
				}
			}
			return false
		case syntax.OpStar, syntax.OpPlus, syntax.OpQuest:
			return w.repeat(node, rest, pos)
		default:
			return true
		}
	}
	return true
}

// repeat tries every run length of a single-character repetition, longest
// first, against the rest of the rule.
func (w *prefilterWalk) repeat(node *syntax.Regexp, rest int32, pos int) bool {
	s := w.s
	sub := node.Sub[0]
	if sub.Op != syntax.OpCharClass && (sub.Op != syntax.OpLiteral || len(sub.Rune) != 1 || sub.Rune[0] >= utf8.RuneSelf) {
		return true
	}
	limit := prefilterRun
	if node.Op == syntax.OpQuest {
		limit = 1
	}
	run := 0
	for run < limit && pos+run < len(s) {
		c := s[pos+run]
		if c >= utf8.RuneSelf {
			return true
		}
		var ok bool
		if sub.Op == syntax.OpCharClass {
			ok = prefilterClassHas(sub.Rune, rune(c))
		} else {
			ok = prefilterLiteral(sub, sub.Rune[0], c)
		}
		if !ok {
			break
		}
		run++
	}
	if run == prefilterRun {
		return true
	}
	minimum := 0
	if node.Op == syntax.OpPlus {
		minimum = 1
	}
	for k := run; k >= minimum; k-- {
		if w.seq(rest, pos+k) {
			return true
		}
	}
	return false
}

func prefilterLiteral(node *syntax.Regexp, r rune, c byte) bool {
	if rune(c) == r {
		return true
	}
	if node.Flags&syntax.FoldCase == 0 {
		return false
	}
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if f == rune(c) {
			return true
		}
	}
	return false
}

func prefilterClassHas(ranges []rune, r rune) bool {
	for i := 0; i+1 < len(ranges); i += 2 {
		if r < ranges[i] {
			return false
		}
		if r <= ranges[i+1] {
			return true
		}
	}
	return false
}

// requiredLiteral returns a literal every match of re must contain, or "" when
// there is none. Only an exact-case ASCII literal qualifies: a folded literal
// matches either case, so a case-sensitive search would reject a real match; a
// part that can match empty cannot be relied on, since every repetition of it
// may take the empty path; and an alternation needs a literal only where all
// branches agree.
func requiredLiteral(re *syntax.Regexp) string {
	switch re.Op {
	case syntax.OpLiteral:
		if len(re.Rune) == 0 || re.Flags&syntax.FoldCase != 0 {
			return ""
		}
		for _, r := range re.Rune {
			if r >= utf8.RuneSelf {
				return ""
			}
		}
		return string(re.Rune)
	case syntax.OpCapture, syntax.OpPlus:
		return requiredLiteral(re.Sub[0])
	case syntax.OpRepeat:
		if re.Min < 1 {
			return ""
		}
		return requiredLiteral(re.Sub[0])
	case syntax.OpConcat:
		best := ""
		for _, sub := range re.Sub {
			if minMatchLen(sub) == 0 {
				continue
			}
			if lit := requiredLiteral(sub); len(lit) > len(best) {
				best = lit
			}
		}
		return best
	case syntax.OpAlternate:
		best := ""
		for i, sub := range re.Sub {
			if lit := requiredLiteral(sub); lit == "" {
				return ""
			} else if i == 0 {
				best = lit
			} else if lit != best {
				return ""
			}
		}
		return best
	}
	return ""
}
