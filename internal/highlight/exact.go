package highlight

import (
	"regexp/syntax"
	"unicode"
	"unicode/utf8"
)

// The exact walk decides a dispatch rule without Go's regexp engine (D523).
// highlight.js 10.7.3 runs each rule as an alternative of one sticky regex;
// the port's per-byte dispatch runs the rules that can begin at a byte as one
// anchored submatcher. Most of those rules are literals, classes, alternations
// and repetitions, for which a backtracking walk over the parsed rule in
// priority order finds the same leftmost-first match RE2 does. The walk
// answers "unknown" for anything whose meaning it does not reproduce (`\A`,
// which the dispatch's slicing changes; counted repetition of a subexpression;
// repetition of a subexpression that can match empty) and when its step
// budget runs out, and the dispatch then runs the regexp as before.

type exactResult uint8

const (
	exactNoMatch exactResult = iota
	exactMatched
	exactUnknown
)

// exactBudget bounds one walk; a rule that needs more steps falls back to
// the regexp, so a nested repetition can not backtrack for long.
const exactBudget = 512

// parseExactPattern parses a prepared rule the way the dispatch compiles it:
// highlight.js always sets the multiline flag, so `^` and `$` are line
// anchors (which the walk leaves undecided), not the text anchors a plain
// Perl parse makes of them.
func parseExactPattern(expr string, caseInsensitive bool) *syntax.Regexp {
	flags := syntax.Perl &^ syntax.OneLine
	if caseInsensitive {
		flags |= syntax.FoldCase
	}
	parsed, err := syntax.Parse(expr, flags)
	if err != nil {
		return nil
	}
	return parsed.Simplify()
}

// exactNode is a parsed rule node compiled for the walk. For a one-rune node
// (a literal of one rune, a class or the dot) ascii holds which ASCII bytes it
// matches, so the common case is one bit test instead of decoding the rune
// and scanning the class ranges or the case folds.
type exactNode struct {
	op        syntax.Op
	nonGreedy bool
	fold      bool
	// single is set on a repetition whose body consumes exactly one rune.
	single   bool
	runes    []rune
	ascii    [2]uint64
	min, max int
	sub      []*exactNode
}

// exactProgram compiles re when every node of it is one the walk reproduces,
// or returns nil.
func exactProgram(re *syntax.Regexp) *exactNode {
	if re == nil || !exactSupported(re) {
		return nil
	}
	return compileExactNode(re)
}

func compileExactNode(re *syntax.Regexp) *exactNode {
	node := &exactNode{
		op:        re.Op,
		nonGreedy: re.Flags&syntax.NonGreedy != 0,
		fold:      re.Flags&syntax.FoldCase != 0,
		runes:     re.Rune,
		min:       re.Min,
		max:       re.Max,
	}
	for _, sub := range re.Sub {
		node.sub = append(node.sub, compileExactNode(sub))
	}
	switch re.Op {
	case syntax.OpStar, syntax.OpPlus, syntax.OpRepeat:
		node.single = exactSingle(re.Sub[0])
	}
	if exactSingle(re) {
		for c := rune(0); c < utf8.RuneSelf; c++ {
			if node.oneRune(c) {
				node.ascii[c>>6] |= 1 << (c & 63)
			}
		}
	}
	return node
}

// oneRune reports whether a one-rune node matches c. The dot's bitmap leaves
// out only \n; exactOne also leaves out JavaScript's other line terminators
// when the input has them.
func (node *exactNode) oneRune(c rune) bool {
	switch node.op {
	case syntax.OpAnyChar:
		return true
	case syntax.OpAnyCharNotNL:
		return c != '\n'
	case syntax.OpLiteral:
		return exactLiteral(node, node.runes[0], c)
	case syntax.OpCharClass:
		return prefilterClassHas(node.runes, c)
	}
	return false
}

func exactSupported(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpEmptyMatch, syntax.OpLiteral, syntax.OpCharClass, syntax.OpAnyChar,
		syntax.OpAnyCharNotNL, syntax.OpWordBoundary, syntax.OpNoWordBoundary, syntax.OpEndText,
		syntax.OpBeginLine, syntax.OpEndLine:
		return true
	case syntax.OpCapture, syntax.OpQuest:
		return exactSupported(re.Sub[0])
	case syntax.OpConcat, syntax.OpAlternate:
		for _, sub := range re.Sub {
			if !exactSupported(sub) {
				return false
			}
		}
		return true
	case syntax.OpStar, syntax.OpPlus:
		if exactSingle(re.Sub[0]) {
			return true
		}
		// An iteration that can match empty is where RE2 and backtracking
		// can disagree; with every iteration consuming input they agree.
		return minMatchLen(re.Sub[0]) > 0 && exactSupported(re.Sub[0])
	case syntax.OpRepeat:
		return exactSingle(re.Sub[0])
	}
	return false
}

// exactSingle reports whether re consumes exactly one rune.
func exactSingle(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpLiteral:
		return len(re.Rune) == 1
	case syntax.OpCharClass, syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return true
	}
	return false
}

// exactCont is the rest of the rule after a node, as a list linked by index
// into the walk's reused slice. A loop continuation (loop set) is a star or
// plus node that may iterate again before next.
type exactCont struct {
	node *exactNode
	next int32
	loop bool
}

type exactWalk struct {
	s string
	// jsLines selects JavaScript's line terminators (\n, \r, U+2028, U+2029)
	// for `^`, `$` and the dot, as the regexp does for an input that has the
	// other three (nonLFLineEnds); otherwise only \n ends a line.
	jsLines bool
	budget  int
	conts   []exactCont
	end     int
}

// matchExactAt runs the walk for re at pos of s. On exactMatched the match
// is s[pos:end].
func matchExactAt(re *exactNode, s string, pos int, jsLines bool, ws *jsWorkspace) (end int, result exactResult) {
	w := exactWalk{s: s, jsLines: jsLines, budget: exactBudget}
	if ws != nil {
		w.conts = ws.exact[:0]
	}
	result = w.seq(w.push(re, prefilterEnd, false), pos)
	if ws != nil {
		ws.exact = w.conts[:0]
	}
	return w.end, result
}

func (w *exactWalk) push(node *exactNode, next int32, loop bool) int32 {
	w.conts = append(w.conts, exactCont{node: node, next: next, loop: loop})
	return int32(len(w.conts) - 1)
}

// seq walks the list from pos. The empty list is the end of the rule.
func (w *exactWalk) seq(seq int32, pos int) exactResult {
	s := w.s
	for seq != prefilterEnd {
		w.budget--
		if w.budget < 0 {
			return exactUnknown
		}
		cont := w.conts[seq]
		node, rest := cont.node, cont.next
		if cont.loop {
			// One more iteration of a star or plus body, preferred unless the
			// repetition is non-greedy.
			again := w.push(node.sub[0], w.push(node, rest, true), false)
			return w.either(node.nonGreedy, again, pos, rest, pos)
		}
		switch node.op {
		case syntax.OpEmptyMatch:
			seq = rest
		case syntax.OpWordBoundary, syntax.OpNoWordBoundary:
			boundary := isWordByteBefore(s, pos) != isWordByteAt(s, pos)
			if boundary != (node.op == syntax.OpWordBoundary) {
				return exactNoMatch
			}
			seq = rest
		case syntax.OpEndText:
			if pos != len(s) {
				return exactNoMatch
			}
			seq = rest
		case syntax.OpBeginLine:
			if pos > 0 && !lineEndBefore(s, pos, w.jsLines) {
				return exactNoMatch
			}
			seq = rest
		case syntax.OpEndLine:
			if pos < len(s) && !lineEndAt(s, pos, w.jsLines) {
				return exactNoMatch
			}
			seq = rest
		case syntax.OpLiteral:
			for _, r := range node.runes {
				// The common case is an ASCII rune against ASCII input, where a
				// byte compare replaces a decode. A folded literal still goes
				// through exactLiteral, which folds both sides.
				if r < utf8.RuneSelf && !node.fold {
					if pos >= len(s) || s[pos] != byte(r) {
						return exactNoMatch
					}
					pos++
					continue
				}
				c, width := utf8.DecodeRuneInString(s[pos:])
				if width == 0 || !exactLiteral(node, r, c) {
					return exactNoMatch
				}
				pos += width
			}
			seq = rest
		case syntax.OpCharClass, syntax.OpAnyChar, syntax.OpAnyCharNotNL:
			width := exactOne(node, s, pos, w.jsLines)
			if width == 0 {
				return exactNoMatch
			}
			pos += width
			seq = rest
		case syntax.OpCapture:
			seq = w.push(node.sub[0], rest, false)
		case syntax.OpConcat:
			for i := len(node.sub) - 1; i >= 0; i-- {
				rest = w.push(node.sub[i], rest, false)
			}
			seq = rest
		case syntax.OpAlternate:
			// Leftmost-first: the first alternative that completes wins, so an
			// undecided one leaves the whole walk undecided.
			for _, alternative := range node.sub {
				if result := w.seq(w.push(alternative, rest, false), pos); result != exactNoMatch {
					return result
				}
			}
			return exactNoMatch
		case syntax.OpQuest:
			with := w.push(node.sub[0], rest, false)
			return w.either(node.nonGreedy, with, pos, rest, pos)
		case syntax.OpStar, syntax.OpPlus, syntax.OpRepeat:
			if node.single {
				return w.run(node, rest, pos)
			}
			if node.op == syntax.OpPlus {
				return w.seq(w.push(node.sub[0], w.push(node, rest, true), false), pos)
			}
			seq = w.push(node, rest, true)
		default:
			return exactUnknown
		}
	}
	w.end = pos
	return exactMatched
}

// either tries the list first at firstPos, then second at secondPos, or the
// other way round when reversed (a non-greedy repetition).
func (w *exactWalk) either(reversed bool, first int32, firstPos int, second int32, secondPos int) exactResult {
	if reversed {
		first, firstPos, second, secondPos = second, secondPos, first, firstPos
	}
	if result := w.seq(first, firstPos); result != exactNoMatch {
		return result
	}
	return w.seq(second, secondPos)
}

// run matches a repetition of a single-rune node, trying the run lengths in
// priority order against the rest of the rule.
func (w *exactWalk) run(node *exactNode, rest int32, pos int) exactResult {
	minimum, maximum := 0, -1
	switch node.op {
	case syntax.OpPlus:
		minimum = 1
	case syntax.OpRepeat:
		minimum, maximum = node.min, node.max
	}
	if node.nonGreedy {
		// Shortest first: extend the run one rune at a time, trying the rest
		// of the rule at each length, so `.*?` reads only as far as it must.
		end := pos
		for count := 0; ; count++ {
			if count >= minimum {
				if result := w.seq(rest, end); result != exactNoMatch {
					return result
				}
			}
			if maximum >= 0 && count >= maximum {
				return exactNoMatch
			}
			width := exactOne(node.sub[0], w.s, end, w.jsLines)
			if width == 0 {
				return exactNoMatch
			}
			end += width
		}
	}
	// Each rune of the scan is charged to the budget, as are the tries
	// against the rest of the rule, so a walk is O(exactBudget) however long
	// the run: the dispatch relies on that to treat a decided rejection as
	// O(1) (D351). ends[k], the position after k repetitions, is only kept
	// once a multi-byte rune makes it differ from pos+k.
	var ends []int
	count := 0
	for end := pos; maximum < 0 || count < maximum; count++ {
		w.budget--
		if w.budget < 0 {
			return exactUnknown
		}
		width := exactOne(node.sub[0], w.s, end, w.jsLines)
		if width == 0 {
			break
		}
		if width > 1 && ends == nil {
			ends = make([]int, count+1, count+8)
			for k := range ends {
				ends[k] = pos + k
			}
		}
		end += width
		if ends != nil {
			ends = append(ends, end)
		}
	}
	if count < minimum {
		return exactNoMatch
	}
	at := func(k int) int {
		if ends == nil {
			return pos + k
		}
		return ends[k]
	}
	for k := count; k >= minimum; k-- {
		if result := w.seq(rest, at(k)); result != exactNoMatch {
			return result
		}
	}
	return exactNoMatch
}

// exactOne returns the width of the rune at pos when node (a one-rune node)
// matches it, or 0.
func exactOne(node *exactNode, s string, pos int, jsLines bool) int {
	if pos >= len(s) {
		return 0
	}
	if b := s[pos]; b < utf8.RuneSelf {
		if node.ascii[b>>6]&(1<<(b&63)) == 0 || (b == '\r' && jsLines && node.op == syntax.OpAnyCharNotNL) {
			return 0
		}
		return 1
	}
	c, width := utf8.DecodeRuneInString(s[pos:])
	if !node.oneRune(c) || (jsLines && node.op == syntax.OpAnyCharNotNL && jsLineEnd(c)) {
		return 0
	}
	return width
}

// lineEndAt reports whether a line terminator starts at pos, and lineEndBefore
// whether one ends there: the m flag's `$` and `^` (jsLineEnd's set under
// jsLines, \n alone otherwise, as the regexp has it).
func lineEndAt(s string, pos int, jsLines bool) bool {
	if s[pos] == '\n' {
		return true
	}
	if !jsLines {
		return false
	}
	c, _ := utf8.DecodeRuneInString(s[pos:])
	return jsLineEnd(c)
}

func lineEndBefore(s string, pos int, jsLines bool) bool {
	if s[pos-1] == '\n' {
		return true
	}
	if !jsLines {
		return false
	}
	c, _ := utf8.DecodeLastRuneInString(s[:pos])
	return jsLineEnd(c)
}

// exactLiteral matches one rune of a literal, folding case like RE2 when the
// literal carries the fold flag.
func exactLiteral(node *exactNode, r, c rune) bool {
	if c == r {
		return true
	}
	if !node.fold {
		return false
	}
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if f == c {
			return true
		}
	}
	return false
}

// isWordByteBefore and isWordByteAt are RE2's ASCII \b test on the runes
// around pos; a multi-byte rune is never a word character.
func isWordByteBefore(s string, pos int) bool {
	return pos > 0 && syntax.IsWordChar(rune(s[pos-1]))
}

func isWordByteAt(s string, pos int) bool {
	return pos < len(s) && syntax.IsWordChar(rune(s[pos]))
}

// modeTreeHasOnEnd reports whether any mode reachable from mode has OnEnd.
func modeTreeHasOnEnd(mode *Mode, seen map[*Mode]bool) bool {
	if mode == nil || seen[mode] {
		return false
	}
	seen[mode] = true
	if mode.OnEnd != nil || modeTreeHasOnEnd(mode.Starts, seen) {
		return true
	}
	for _, list := range [][]*Mode{mode.Contains, mode.Variants} {
		for _, child := range list {
			if modeTreeHasOnEnd(child, seen) {
				return true
			}
		}
	}
	return false
}
