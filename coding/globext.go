package coding

import (
	"slices"
)

// Extended globs (`@(a|b)`, `?(...)`, `+(...)`, `*(...)`, `!(...)`) in one
// path segment, ported from @isaacs/nonrecursive-glob-match 1.0.3
// (src/index.ts), which minimatch 11.0.3 uses for every portion that is not a
// plain literal (pi v1.6.0, faac5858, resolves that version). The pattern
// compiles to the same node graph and runs the same thread simulation; the
// reference's DFA cache, linear fast path and length/affix prechecks only
// skip work and are left out. Character classes reuse matchClass and case
// folding reuses the ASCII folding of matchGlobSegment, so a segment means
// the same thing on either path.

type globExtKind uint8

const (
	globExtLit globExtKind = iota
	globExtAny
	globExtStar
	globExtClass
	globExtSplit
	globExtNeg
	globExtMatch
)

type globExtNode struct {
	kind  globExtKind
	id    int
	ch    rune
	class []rune
	next  *globExtNode
	sub   *globExtNode
	out   []*globExtNode
}

type globExtItem struct {
	kind    globExtKind // lit, any, star, class, or split for an extglob
	ch      rune
	class   []rune
	extType rune
	alts    [][]globExtItem
}

type globExtTokenKind uint8

const (
	globTokItem globExtTokenKind = iota
	globTokOpen
	globTokBar
	globTokClose
)

type globExtToken struct {
	kind    globExtTokenKind
	item    globExtItem
	extType rune
}

const globExtTypes = "!?+*@"

// globExtTokenize is the reference's tokenize with extglobs enabled.
func globExtTokenize(pattern []rune) []globExtToken {
	var tokens []globExtToken
	lit := func(char rune) globExtToken {
		return globExtToken{kind: globTokItem, item: globExtItem{kind: globExtLit, ch: char}}
	}
	for i := 0; i < len(pattern); {
		char := pattern[i]
		switch {
		case char == '\\':
			// A trailing backslash is a literal backslash.
			if i+1 < len(pattern) {
				tokens = append(tokens, lit(pattern[i+1]))
				i += 2
			} else {
				tokens = append(tokens, lit(char))
				i++
			}
			continue
		case char == '[':
			if consumed, _ := matchClass(pattern[i:], 0); consumed > 1 {
				tokens = append(tokens, globExtToken{kind: globTokItem, item: globExtItem{kind: globExtClass, class: pattern[i : i+consumed]}})
				i += consumed
				continue
			}
		case i+1 < len(pattern) && pattern[i+1] == '(' && containsRune(globExtTypes, char):
			tokens = append(tokens, globExtToken{kind: globTokOpen, extType: char})
			i += 2
			continue
		}
		switch char {
		case '|':
			tokens = append(tokens, globExtToken{kind: globTokBar})
		case ')':
			tokens = append(tokens, globExtToken{kind: globTokClose})
		case '*':
			tokens = append(tokens, globExtToken{kind: globTokItem, item: globExtItem{kind: globExtStar}})
		case '?':
			tokens = append(tokens, globExtToken{kind: globTokItem, item: globExtItem{kind: globExtAny}})
		default:
			tokens = append(tokens, lit(char))
		}
		i++
	}
	return tokens
}

func containsRune(set string, char rune) bool {
	for _, candidate := range set {
		if candidate == char {
			return true
		}
	}
	return false
}

// globExtPairs maps each open token to its close token.
func globExtPairs(tokens []globExtToken) map[int]int {
	pairs := map[int]int{}
	var stack []int
	for i, token := range tokens {
		switch {
		case token.kind == globTokOpen:
			stack = append(stack, i)
		case token.kind == globTokClose && len(stack) > 0:
			pairs[stack[len(stack)-1]] = i
			stack = stack[:len(stack)-1]
		}
	}
	return pairs
}

// globExtBuild builds the alternatives between start and end; an open token
// that is not closed, and a bar or close outside a closed extglob, is literal.
func globExtBuild(tokens []globExtToken, pairs map[int]int, start, end int, top bool) [][]globExtItem {
	alts := [][]globExtItem{nil}
	lit := func(char rune) globExtItem { return globExtItem{kind: globExtLit, ch: char} }
	for i := start; i < end; i++ {
		token := tokens[i]
		last := len(alts) - 1
		closeAt, closed := pairs[i]
		switch {
		case token.kind == globTokOpen && closed:
			sub := globExtBuild(tokens, pairs, i+1, closeAt, false)
			alts[last] = append(alts[last], globExtItem{kind: globExtSplit, extType: token.extType, alts: sub})
			i = closeAt
		case token.kind == globTokBar && !top:
			alts = append(alts, nil)
		case token.kind == globTokOpen:
			alts[last] = append(alts[last], lit(token.extType), lit('('))
		case token.kind == globTokBar:
			alts[last] = append(alts[last], lit('|'))
		case token.kind == globTokClose:
			alts[last] = append(alts[last], lit(')'))
		default:
			alts[last] = append(alts[last], token.item)
		}
	}
	return alts
}

// globExtCompile is the reference's compile: the start node of the graph,
// whose shared match node has id 0.
func globExtCompile(items []globExtItem) *globExtNode {
	match := &globExtNode{kind: globExtMatch}
	id := 1
	newNode := func(node *globExtNode) *globExtNode {
		node.id = id
		id++
		return node
	}
	split := func(out []*globExtNode) *globExtNode {
		return newNode(&globExtNode{kind: globExtSplit, out: out})
	}
	var item func(it globExtItem, next *globExtNode) *globExtNode
	seq := func(items []globExtItem, next *globExtNode) *globExtNode {
		for _, it := range slices.Backward(items) {
			next = item(it, next)
		}
		return next
	}
	item = func(it globExtItem, next *globExtNode) *globExtNode {
		switch it.kind {
		case globExtLit:
			return newNode(&globExtNode{kind: globExtLit, ch: it.ch, next: next})
		case globExtAny, globExtStar:
			return newNode(&globExtNode{kind: it.kind, next: next})
		case globExtClass:
			return newNode(&globExtNode{kind: globExtClass, class: it.class, next: next})
		}
		alts := func(next *globExtNode) []*globExtNode {
			out := make([]*globExtNode, 0, len(it.alts))
			for _, alt := range it.alts {
				out = append(out, seq(alt, next))
			}
			return out
		}
		switch it.extType {
		case '@':
			return split(alts(next))
		case '?':
			return split(append([]*globExtNode{next}, alts(next)...))
		case '!':
			return newNode(&globExtNode{kind: globExtNeg, sub: split(alts(match)), next: next})
		}
		// * and +: loop back after each match.
		after := split(nil)
		entry := split(alts(after))
		after.out = append(after.out, next, entry)
		if it.extType == '*' {
			return after
		}
		return entry
	}
	return seq(items, match)
}

// globExtRun is a thread inside a !(...) negation: the threads of the negated
// sub-pattern, started where the negation started.
type globExtRun struct {
	neg   *globExtNode
	state *globExtState
}

type globExtState struct {
	threads []*globExtNode
	match   bool
	runs    []globExtRun
	ids     []int
	has     map[*globExtNode]bool
	seen    map[*globExtNode]bool
}

func newGlobExtState() *globExtState {
	return &globExtState{has: map[*globExtNode]bool{}, seen: map[*globExtNode]bool{}}
}

func (s *globExtState) dead() bool {
	return len(s.threads) == 0 && !s.match && len(s.runs) == 0
}

func (s *globExtState) done() *globExtState {
	s.ids = s.ids[:0]
	if s.match {
		s.ids = append(s.ids, 0)
	}
	for _, thread := range s.threads {
		s.ids = append(s.ids, thread.id)
	}
	slices.Sort(s.ids)
	return s
}

func sameGlobExtState(a, b *globExtState) bool {
	if !slices.Equal(a.ids, b.ids) || len(a.runs) != len(b.runs) {
		return false
	}
	for _, run := range a.runs {
		if !slices.ContainsFunc(b.runs, func(other globExtRun) bool {
			return other.neg == run.neg && sameGlobExtState(other.state, run.state)
		}) {
			return false
		}
	}
	return true
}

type globExtSim struct {
	// dots: the value is "." or "..", which only literal dots match.
	dots bool
	// dotStart: the value starts with "." (minimatch's dot option is off).
	dotStart bool
}

// restricted reports whether a wildcard may not match at index i; inside a
// negated sub-pattern it never applies, since the negation itself can not
// match a leading dot.
func (sim globExtSim) restricted(i int, top bool) bool {
	return top && (sim.dots || (sim.dotStart && i == 0))
}

func (sim globExtSim) add(state *globExtState, node *globExtNode, i int, top bool) {
	switch node.kind {
	case globExtSplit:
		if state.seen[node] {
			return
		}
		state.seen[node] = true
		for _, out := range node.out {
			sim.add(state, out, i, top)
		}
	case globExtNeg:
		if state.seen[node] {
			return
		}
		state.seen[node] = true
		sub := newGlobExtState()
		sim.add(sub, node.sub, i, false)
		sim.addRun(state, globExtRun{neg: node, state: sub.done()}, i, top)
	case globExtStar:
		// A * can not start a dotfile, even if it matches nothing.
		if sim.restricted(i, top) {
			return
		}
		if sim.addThread(state, node) {
			sim.add(state, node.next, i, top)
		}
	case globExtMatch:
		state.match = true
	default:
		sim.addThread(state, node)
	}
}

func (sim globExtSim) addThread(state *globExtState, node *globExtNode) bool {
	if state.has[node] {
		return false
	}
	state.has[node] = true
	state.threads = append(state.threads, node)
	return true
}

func (sim globExtSim) addRun(state *globExtState, run globExtRun, i int, top bool) {
	for _, existing := range state.runs {
		if existing.neg == run.neg && sameGlobExtState(existing.state, run.state) {
			return
		}
	}
	state.runs = append(state.runs, run)
	if !run.state.match {
		sim.add(state, run.neg.next, i, top)
	}
}

// step returns the threads after char at index i.
func (sim globExtSim) step(state *globExtState, char rune, i int, top bool) *globExtState {
	next := newGlobExtState()
	for _, thread := range state.threads {
		switch thread.kind {
		case globExtLit:
			if thread.ch == char {
				sim.add(next, thread.next, i+1, top)
			}
		case globExtAny:
			if !sim.restricted(i, top) {
				sim.add(next, thread.next, i+1, top)
			}
		case globExtClass:
			if !sim.restricted(i, top) {
				if _, ok := matchClass(thread.class, char); ok {
					sim.add(next, thread.next, i+1, top)
				}
			}
		case globExtStar:
			if !sim.restricted(i, top) {
				sim.add(next, thread, i+1, top)
			}
		}
	}
	if !sim.restricted(i, top) {
		for _, run := range state.runs {
			// A sub-pattern with no threads can not match again, so the run
			// stays the same.
			if run.state.dead() {
				sim.addRun(next, run, i+1, top)
				continue
			}
			sub := sim.step(run.state, char, i, false)
			sim.addRun(next, globExtRun{neg: run.neg, state: sub}, i+1, top)
		}
	}
	return next.done()
}

// globExtSegment compiles a segment that holds a closed extglob, or returns
// nil when it holds none (or is the literal `?()`-style shape the reference
// reads with extglobs off), so the plain segment matcher applies.
func globExtSegment(pattern []rune) *globExtNode {
	if len(pattern) == 3 && containsRune("?*+@", pattern[0]) && pattern[1] == '(' && pattern[2] == ')' {
		return nil
	}
	tokens := globExtTokenize(pattern)
	pairs := globExtPairs(tokens)
	if len(pairs) == 0 {
		return nil
	}
	return globExtCompile(globExtBuild(tokens, pairs, 0, len(tokens), true)[0])
}

// matchGlobExt runs the compiled segment over a non-empty value.
func matchGlobExt(start *globExtNode, value []rune) bool {
	sim := globExtSim{}
	if text := string(value); text == "." || text == ".." {
		sim.dots = true
	} else {
		sim.dotStart = value[0] == '.'
	}
	state := newGlobExtState()
	sim.add(state, start, 0, true)
	state.done()
	for i, char := range value {
		if state.dead() {
			return false
		}
		state = sim.step(state, char, i, true)
	}
	return state.match
}
