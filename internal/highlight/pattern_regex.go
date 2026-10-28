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
type patternRegexp struct {
	*regexp.Regexp
	lineSensitive bool
	once          sync.Once
	program       *syntax.Prog
}

func nonLFLineEnds(s string) bool { return strings.ContainsAny(s, "\r\u2028\u2029") }

func jsLineEnd(r rune) bool { return r == '\n' || r == '\r' || r == '\u2028' || r == '\u2029' }

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
	if !jsLines || !re.lineSensitive {
		return re.Regexp.FindStringSubmatchIndex(s)
	}
	re.once.Do(re.compileJS)
	if re.program == nil {
		return re.Regexp.FindStringSubmatchIndex(s)
	}
	return runJSProgram(re.program, s, ws)
}

func (re *patternRegexp) findSubmatch(s string, jsLines bool) []int {
	return re.findSubmatchWS(s, jsLines, nil)
}

func (re *patternRegexp) FindStringSubmatchIndex(s string) []int {
	return re.findSubmatch(s, re.lineSensitive && nonLFLineEnds(s))
}

func (re *patternRegexp) findIndexWS(s string, jsLines bool, ws *jsWorkspace) []int {
	if !jsLines || !re.lineSensitive {
		return re.Regexp.FindStringIndex(s)
	}
	if match := re.findSubmatchWS(s, true, ws); match != nil {
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

func (re *patternRegexp) FindAllStringIndex(s string, n int) [][]int {
	if !re.lineSensitive || !nonLFLineEnds(s) {
		return re.Regexp.FindAllStringIndex(s, n)
	}
	var matches [][]int
	previousEnd := -1
	for pos := 0; pos <= len(s) && (n < 0 || len(matches) < n); {
		match := re.findIndex(s[pos:], true)
		if match == nil {
			break
		}
		start, end := pos+match[0], pos+match[1]
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

func runJSProgram(prog *syntax.Prog, s string, ws *jsWorkspace) []int {
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
	for pos := 0; ; {
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
