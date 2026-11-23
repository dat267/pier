package highlight

import (
	"regexp"
	"regexp/syntax"
	"strings"
	"sync"
	"unicode/utf8"
)

// patternRegexp retains RE2 for LF input. For JavaScript's additional line
// terminators it executes the same regular language with JS assertions and
// dots, without normalizing text or consuming an assertion's boundary.
// The program is immutable after compilation; matching storage is call-owned.
// Matching input is always the code-unit encoding from codeunits.go; public
// regexp-style methods therefore expect encoded text.
type patternRegexp struct {
	*regexp.Regexp
	lineSensitive bool
	once          sync.Once
	program       *syntax.Prog
}

// nonLFLineEnds reports whether s has a line terminator other than \n.
// strings.ContainsAny decodes its argument's runes and so searches rune by
// rune; three byte searches are an order of magnitude faster on long input.
func nonLFLineEnds(s string) bool {
	return strings.IndexByte(s, '\r') >= 0 || strings.Contains(s, "\u2028") || strings.Contains(s, "\u2029")
}

func jsLineEnd(r rune) bool { return r == '\n' || r == '\r' || r == '\u2028' || r == '\u2029' }

// isJSSpaceByte reports whether an ASCII byte is JavaScript `\s`. A byte at or
// above utf8.RuneSelf is not whitespace on its own: the multi-byte members of
// `\s` (U+00A0 and the rest) must be checked with jsSpaceWidth.
func isJSSpaceByte(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	}
	return false
}

// jsSpaceWidth returns the width of the JavaScript `\s` character at s[i], or 0
// when there is none. `\s` is not ASCII-only: U+00A0, U+1680, U+2000-200A,
// U+2028, U+2029, U+202F, U+205F, U+3000 and U+FEFF are members.
func jsSpaceWidth(s string, i int) int {
	if i >= len(s) {
		return 0
	}
	if c := s[i]; c < utf8.RuneSelf {
		if isJSSpaceByte(c) {
			return 1
		}
		return 0
	}
	r, width := utf8.DecodeRuneInString(s[i:])
	switch r {
	case 0x00A0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return width
	}
	if r >= 0x2000 && r <= 0x200A {
		return width
	}
	return 0
}

// spaceToLineEnd is `\A\s*$` under the m flag: `\s` covers the line
// terminators and `$` is a line end, so it holds when the leading whitespace
// reaches the end of s or contains a line terminator. Hand-written because an
// `\A`-anchored regexp with an unbounded `\s*` costs Go a pass over the
// whole remainder per call (D521).
func spaceToLineEnd(s string, jsLines bool) bool {
	for i := 0; ; {
		if i == len(s) || lineEndAt(s, i, jsLines) {
			return true
		}
		w := jsSpaceWidth(s, i)
		if w == 0 {
			return false
		}
		i += w
	}
}

// skipJSSpace returns the offset after every JavaScript whitespace character
// starting at i.
func skipJSSpace(s string, i int) int {
	for {
		w := jsSpaceWidth(s, i)
		if w == 0 {
			return i
		}
		i += w
	}
}

func (re *patternRegexp) compileJS() {
	parsed, err := syntax.Parse(re.String(), syntax.Perl)
	if err != nil {
		panic(err) // The same expression already passed regexp.Compile.
	}
	changed := false
	var adjust func(*syntax.Regexp)
	adjust = func(node *syntax.Regexp) {
		switch node.Op {
		case syntax.OpBeginLine, syntax.OpEndLine:
			changed = true
		case syntax.OpAnyCharNotNL:
			// Rewrite DOT, not InstRuneAnyNotNL: the compiler also uses that
			// instruction for explicit [^\n], whose meaning must not change.
			node.Op = syntax.OpCharClass
			node.Rune = []rune{0, '\n' - 1, '\n' + 1, '\r' - 1, '\r' + 1, '\u2028' - 1, '\u2029' + 1, utf8.MaxRune}
			changed = true
		}
		for _, child := range node.Sub {
			adjust(child)
		}
	}
	adjust(parsed)
	if changed {
		re.program, err = syntax.Compile(parsed.Simplify())
		if err != nil {
			panic(err)
		}
	}
}

func (re *patternRegexp) findSubmatchWS(s string, jsLines bool, ws *jsWorkspace) []int {
	return re.findSubmatchWSFrom(s, 0, jsLines, ws)
}

// findSubmatchWSFrom starts the search at start while keeping the text before
// it as assertion context: an anchor at start must see the real preceding
// character, not a fresh string boundary (JS's ^ would otherwise match at every
// restart position).
func (re *patternRegexp) findSubmatchWSFrom(s string, start int, jsLines bool, ws *jsWorkspace) []int {
	if !jsLines || !re.lineSensitive {
		match := re.Regexp.FindStringSubmatchIndex(s[start:])
		if match == nil {
			return nil
		}
		for i, index := range match {
			if index >= 0 {
				match[i] = index + start
			}
		}
		return match
	}
	re.once.Do(re.compileJS)
	if re.program == nil {
		return re.findSubmatchWSFrom(s, start, false, ws)
	}
	return runJSProgram(re.program, s, start, ws)
}

func (re *patternRegexp) findSubmatch(s string, jsLines bool) []int {
	return re.findSubmatchWS(s, jsLines, nil)
}

func (re *patternRegexp) FindStringSubmatchIndex(s string) []int {
	return re.findSubmatch(s, re.lineSensitive && nonLFLineEnds(s))
}

func (re *patternRegexp) findIndexWS(s string, jsLines bool, ws *jsWorkspace) []int {
	return re.findIndexWSFrom(s, 0, jsLines, ws)
}

func (re *patternRegexp) findIndexWSFrom(s string, start int, jsLines bool, ws *jsWorkspace) []int {
	if !jsLines || !re.lineSensitive {
		if match := re.Regexp.FindStringIndex(s[start:]); match != nil {
			return []int{match[0] + start, match[1] + start}
		}
		return nil
	}
	if match := re.findSubmatchWSFrom(s, start, true, ws); match != nil {
		return match[:2]
	}
	return nil
}

func (re *patternRegexp) findIndex(s string, jsLines bool) []int {
	return re.findIndexWS(s, jsLines, nil)
}

func (re *patternRegexp) FindStringIndex(s string) []int {
	return re.findIndex(s, re.lineSensitive && nonLFLineEnds(s))
}

func (re *patternRegexp) MatchString(s string) bool { return re.FindStringIndex(s) != nil }

// MatchStringWS matches with line sensitivity the caller already derived from
// the document, so a guard callback does not rescan the remaining tail.
func (re *patternRegexp) MatchStringWS(s string, jsLines bool) bool {
	return re.findIndexWS(s, jsLines && re.lineSensitive, nil) != nil
}

func (re *patternRegexp) FindAllStringIndex(s string, n int) [][]int {
	if !re.lineSensitive || !nonLFLineEnds(s) {
		return re.Regexp.FindAllStringIndex(s, n)
	}
	var matches [][]int
	previousEnd := -1
	for pos := 0; pos <= len(s) && (n < 0 || len(matches) < n); {
		match := re.findIndexWSFrom(s, pos, true, nil)
		if match == nil {
			break
		}
		// The search already reports absolute offsets into s.
		start, end := match[0], match[1]
		if start != end || start != previousEnd {
			matches = append(matches, []int{start, end})
		}
		previousEnd = end
		pos = end
		if start == end {
			if end == len(s) {
				break
			}
			_, width := utf8.DecodeRuneInString(s[end:])
			pos += width
		}
	}
	return matches
}

// jsWorkspace is call-private matching storage. A highlighter reuses one
// across matches so a CRLF document does not allocate queue and visit tables
// for every lexeme; it is never shared between concurrent highlights.
type jsWorkspace struct {
	seen   []int
	epoch  int
	queues [2]jsQueue
	best   []int
	start  []int
	// prefilter is the dispatch prefilter's reused continuation list.
	prefilter []prefilterCont
	exact     []exactCont
}

type jsQueue struct {
	pcs  []uint32
	caps []int
}

func (q *jsQueue) begin(ws *jsWorkspace, progLen int) {
	if cap(ws.seen) < progLen {
		ws.seen = make([]int, progLen)
	} else {
		ws.seen = ws.seen[:progLen]
		clear(ws.seen)
	}
	ws.epoch = 0
}

func (ws *jsWorkspace) nextEpoch() int {
	ws.epoch++
	if ws.epoch == int(^uint(0)>>1) {
		clear(ws.seen)
		ws.epoch = 1
	}
	return ws.epoch
}

func (q *jsQueue) reset() {
	q.pcs, q.caps = q.pcs[:0], q.caps[:0]
}

func (q *jsQueue) add(prog *syntax.Prog, ws *jsWorkspace, epoch int, pc uint32, caps []int, pos int, context syntax.EmptyOp) {
	if ws.seen[pc] == epoch {
		return
	}
	ws.seen[pc] = epoch
	inst := &prog.Inst[pc]
	switch inst.Op {
	case syntax.InstFail:
	case syntax.InstAlt, syntax.InstAltMatch:
		q.add(prog, ws, epoch, inst.Out, caps, pos, context)
		q.add(prog, ws, epoch, inst.Arg, caps, pos, context)
	case syntax.InstCapture:
		old := caps[inst.Arg]
		caps[inst.Arg] = pos
		q.add(prog, ws, epoch, inst.Out, caps, pos, context)
		caps[inst.Arg] = old
	case syntax.InstEmptyWidth:
		if syntax.EmptyOp(inst.Arg)&context == syntax.EmptyOp(inst.Arg) {
			q.add(prog, ws, epoch, inst.Out, caps, pos, context)
		}
	case syntax.InstNop:
		q.add(prog, ws, epoch, inst.Out, caps, pos, context)
	default: // A consuming instruction or Match.
		q.pcs = append(q.pcs, pc)
		q.caps = append(q.caps, caps...)
	}
}

func jsEmptyContext(before, after rune) syntax.EmptyOp {
	context := syntax.EmptyOpContext(before, after)
	if jsLineEnd(before) {
		context |= syntax.EmptyBeginLine
	}
	if jsLineEnd(after) {
		context |= syntax.EmptyEndLine
	}
	return context
}

func runJSProgram(prog *syntax.Prog, s string, start int, ws *jsWorkspace) []int {
	var local jsWorkspace
	if ws == nil {
		ws = &local
	}
	ws.queues[0].begin(ws, len(prog.Inst))
	current, next := &ws.queues[0], &ws.queues[1]
	current.reset()
	next.reset()
	ncap := prog.NumCap
	if cap(ws.start) < ncap {
		ws.start = make([]int, ncap)
	}
	initial := ws.start[:ncap]
	for i := range initial {
		initial[i] = -1
	}
	ws.best = ws.best[:0]
	before := rune(-1)
	if start > 0 {
		before, _ = utf8.DecodeLastRuneInString(s[:start])
	}
	for pos := start; ; {
		after, width := rune(-1), 0
		if pos < len(s) {
			after, width = utf8.DecodeRuneInString(s[pos:])
		}
		if len(ws.best) == 0 {
			initial[0] = pos
			current.add(prog, ws, ws.nextEpoch(), uint32(prog.Start), initial, pos, jsEmptyContext(before, after))
		}
		limit := len(current.pcs)
		for i, pc := range current.pcs {
			if prog.Inst[pc].Op == syntax.InstMatch {
				ws.best = append(ws.best[:0], current.caps[i*ncap:(i+1)*ncap]...)
				ws.best[1] = pos
				limit = i // Only higher-priority threads may improve this match.
				break
			}
		}
		if pos == len(s) || len(ws.best) > 0 && limit == 0 {
			if len(ws.best) == 0 {
				return nil
			}
			return ws.best
		}
		following := rune(-1)
		if pos+width < len(s) {
			following, _ = utf8.DecodeRuneInString(s[pos+width:])
		}
		next.reset()
		context := jsEmptyContext(after, following)
		// One visit epoch per destination position, not per thread: duplicate
		// thread states at the same position must collapse to the highest-
		// priority capture set instead of multiplying across alternatives.
		destinationEpoch := ws.nextEpoch()
		for i, pc := range current.pcs[:limit] {
			inst := &prog.Inst[pc]
			matched := false
			switch inst.Op {
			case syntax.InstRune, syntax.InstRune1:
				matched = inst.MatchRune(after)
			case syntax.InstRuneAny:
				matched = true
			case syntax.InstRuneAnyNotNL:
				matched = after != '\n'
			}
			if matched {
				next.add(prog, ws, destinationEpoch, inst.Out, current.caps[i*ncap:(i+1)*ncap], pos+width, context)
			}
		}
		current, next = next, current
		pos, before = pos+width, after
		if len(current.pcs) == 0 && len(ws.best) > 0 {
			return ws.best
		}
	}
}
