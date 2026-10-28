package highlight

import (
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

// This file ports highlight.js's `_highlight` scan loop (core.js).

// Node is one node of the highlighted token tree: a scope node (Kind set,
// Children present) or a text leaf (Kind empty, Text set). The tree mirrors the
// reference's TokenTree, so pier can map scopes to styles without the
// reference's HTML round trip.
type Node struct {
	Kind        string
	Sublanguage bool
	Children    []*Node
	Text        string
}

// Result is the outcome of highlighting one input with one language.
type Result struct {
	Language  string
	Relevance int
	Nodes     []*Node
	Illegal   bool

	// top is the mode instance the scan finished in, used to continue a named
	// sublanguage across segments.
	top *modeInstance
}

type modeInstance struct {
	mode       *compiledMode
	parent     *modeInstance
	endRe      *patternRegexp
	data       map[string]string
	regexIndex int
}

// emitter ports TokenTreeEmitter.
type emitter struct {
	root  *Node
	stack []*Node
}

func newEmitter() *emitter {
	root := &Node{}
	return &emitter{root: root, stack: []*Node{root}}
}

func (e *emitter) openNode(kind string) {
	node := &Node{Kind: kind}
	top := e.stack[len(e.stack)-1]
	top.Children = append(top.Children, node)
	e.stack = append(e.stack, node)
}

func (e *emitter) closeNode() {
	if len(e.stack) > 1 {
		e.stack = e.stack[:len(e.stack)-1]
	}
}

func (e *emitter) closeAllNodes() {
	for len(e.stack) > 1 {
		e.closeNode()
	}
}

func (e *emitter) addText(text string) {
	if text == "" {
		return
	}
	top := e.stack[len(e.stack)-1]
	top.Children = append(top.Children, &Node{Text: text})
}

func (e *emitter) addKeyword(text, kind string) {
	if text == "" {
		return
	}
	e.openNode(kind)
	e.addText(text)
	e.closeNode()
}

func (e *emitter) addSublanguage(other *emitter, name string) {
	other.root.Kind = name
	other.root.Sublanguage = true
	top := e.stack[len(e.stack)-1]
	top.Children = append(top.Children, other.root)
}

type highlighter struct {
	lang             *compiledLanguage
	rootMode         *compiledMode
	top              *modeInstance
	emitter          *emitter
	modeBuffer       string
	relevance        float64
	index            int
	iterations       int
	resumeScanAtSame bool
	lastMatch        *match
	continuations    map[string]*modeInstance
	ignoreIllegals   bool
	illegal          bool
	jsLines          bool
	regexWS          jsWorkspace
}

const noMatch = -1

func highlightWith(lang *compiledLanguage, code string, ignoreIllegals bool) *Result {
	return highlightWithContinuation(lang, code, ignoreIllegals, nil)
}

// highlightWithContinuation resumes a named sublanguage's previous top mode,
// as the reference's `_highlight(..., continuation)` does.
func highlightWithContinuation(lang *compiledLanguage, code string, ignoreIllegals bool, continuation *modeInstance) *Result {
	h := &highlighter{
		lang:           lang,
		rootMode:       lang.root,
		emitter:        newEmitter(),
		continuations:  map[string]*modeInstance{},
		ignoreIllegals: ignoreIllegals,
		jsLines:        nonLFLineEnds(code),
	}
	if continuation != nil {
		h.top = continuation
	} else {
		h.top = &modeInstance{mode: lang.root, data: map[string]string{}}
	}
	h.processContinuations()
	h.top.regexIndex = 0

	for {
		h.iterations++
		if h.resumeScanAtSame {
			h.resumeScanAtSame = false
		} else {
			h.top.regexIndex = 0
		}
		m, next := h.top.mode.matcher.exec(code, h.index, h.top.regexIndex, h.jsLines, &h.regexWS)
		h.top.regexIndex = next
		if m == nil {
			break
		}
		before := code[h.index:m.index]
		processed := h.processLexeme(before, m)
		if h.illegal {
			break
		}
		h.index = min(m.index+processed,
			// A zero-width match at the end advances one past it; JavaScript's
			// substr clamps, Go slicing does not.
			len(code))
		if h.iterations > 1000000 {
			break
		}
	}
	if h.illegal {
		// The reference aborts the whole highlight and returns the input as
		// plain text when an illegal lexeme is found and illegal matches are not
		// being ignored.
		return &Result{
			Language: lang.name,
			Illegal:  true,
			Nodes:    []*Node{{Text: code}},
		}
	}
	h.processLexeme(code[h.index:], nil)
	h.emitter.closeAllNodes()

	return &Result{
		Language:  lang.name,
		Relevance: floorRelevance(h.relevance),
		Nodes:     h.emitter.root.Children,
		top:       h.top,
	}
}

func (h *highlighter) processContinuations() {
	var list []string
	for cur := h.top; cur != nil && cur.mode != h.rootMode; cur = cur.parent {
		if cur.mode.className != "" {
			list = append([]string{cur.mode.className}, list...)
		}
	}
	for _, item := range list {
		h.emitter.openNode(item)
	}
}

func (h *highlighter) processKeywords() {
	top := h.top
	if top.mode.keywords == nil {
		h.emitter.addText(h.modeBuffer)
		return
	}
	if top.mode.keywordWord {
		h.processKeywordWords()
		return
	}
	matches := top.mode.keywordPatternRe.FindAllStringIndex(h.modeBuffer, -1)
	var buf strings.Builder
	last := 0
	for _, m := range matches {
		buf.WriteString(h.modeBuffer[last:m[0]])
		text := h.modeBuffer[m[0]:m[1]]
		key := text
		if h.lang.caseInsensitive {
			key = strings.ToLower(key)
		}
		if data, ok := top.mode.keywords[key]; ok {
			h.emitter.addText(buf.String())
			buf.Reset()
			h.relevance += data.relevance
			if strings.HasPrefix(data.className, "_") {
				buf.WriteString(text)
			} else {
				className := data.className
				if alias, ok := h.lang.classNameAliases[className]; ok {
					className = alias
				}
				h.emitter.addKeyword(text, className)
			}
		} else {
			buf.WriteString(text)
		}
		last = m[1]
	}
	buf.WriteString(h.modeBuffer[last:])
	h.emitter.addText(buf.String())
}

// processKeywordWords scans `\w+` keyword runs directly, avoiding the
// regexp engine and its result slice for the common default pattern.
func (h *highlighter) processKeywordWords() {
	top := h.top
	buf := h.modeBuffer
	var text strings.Builder
	i := 0
	for i < len(buf) {
		if !isWordByte(buf[i]) {
			text.WriteByte(buf[i])
			i++
			continue
		}
		start := i
		for i < len(buf) && isWordByte(buf[i]) {
			i++
		}
		word := buf[start:i]
		key := word
		if h.lang.caseInsensitive {
			key = strings.ToLower(key)
		}
		if data, ok := top.mode.keywords[key]; ok {
			h.emitter.addText(text.String())
			text.Reset()
			h.relevance += data.relevance
			if strings.HasPrefix(data.className, "_") {
				text.WriteString(word)
			} else {
				className := data.className
				if alias, ok := h.lang.classNameAliases[className]; ok {
					className = alias
				}
				h.emitter.addKeyword(word, className)
			}
		} else {
			text.WriteString(word)
		}
	}
	h.emitter.addText(text.String())
}

func (h *highlighter) processSubLanguage() {
	if h.modeBuffer == "" {
		return
	}
	var result *Result
	name := ""
	if len(h.top.mode.subLanguage) == 1 {
		name = h.top.mode.subLanguage[0]
		sub, err := getCompiledLanguage(name)
		if err != nil {
			h.emitter.addText(h.modeBuffer)
			return
		}
		result = highlightWithContinuation(sub, h.modeBuffer, true, h.continuations[name])
		h.continuations[name] = result.top
	} else {
		result, name = highlightAuto(h.modeBuffer, h.top.mode.subLanguage)
		if result == nil {
			h.emitter.addText(h.modeBuffer)
			return
		}
	}
	if h.top.mode.relevance > 0 {
		h.relevance += float64(result.Relevance)
	}
	h.emitter.addSublanguage(nodesEmitter(result.Nodes), name)
}

func (h *highlighter) processBuffer() {
	if h.top.mode.subLanguage != nil {
		h.processSubLanguage()
	} else {
		h.processKeywords()
	}
	h.modeBuffer = ""
}

func (h *highlighter) startNewMode(mode *compiledMode, endRe *patternRegexp, data map[string]string) {
	if mode.className != "" {
		className := mode.className
		if alias, ok := h.lang.classNameAliases[className]; ok {
			className = alias
		}
		h.emitter.openNode(className)
	}
	if data == nil {
		data = map[string]string{}
	}
	h.top = &modeInstance{mode: mode, parent: h.top, endRe: endRe, data: data}
}

func (h *highlighter) endOfMode(mode *modeInstance, m *match, remainder string) *modeInstance {
	endRe := mode.endRe
	if endRe == nil {
		endRe = mode.mode.endRe
	}
	matched := endRe != nil && h.lineStartsWith(endRe, remainder)
	if matched {
		if mode.mode.onEnd != nil {
			resp := &Response{data: mode.data}
			mode.mode.onEnd(m, resp)
			if resp.isMatchIgnored {
				matched = false
			}
		}
		if matched {
			for mode.mode.endsParent && mode.parent != nil {
				mode = mode.parent
			}
			return mode
		}
	}
	if mode.mode.endsWithParent && mode.parent != nil {
		return h.endOfMode(mode.parent, m, remainder)
	}
	return nil
}

func (h *highlighter) doIgnore(lexeme string) int {
	// An ignored begin did not enter its mode, so it must not count as the last
	// begin for the zero-width begin/end collision guard in processLexeme; that
	// guard would otherwise let an ignored lookahead-guard rule suppress the
	// real end match at the same index.
	h.lastMatch = nil
	if h.top.regexIndex == 0 {
		if lexeme != "" {
			_, width := utf8.DecodeRuneInString(lexeme)
			h.modeBuffer += lexeme[:width]
			return width
		}
		return 1
	}
	h.resumeScanAtSame = true
	return 0
}

func (h *highlighter) doBeginMatch(m *match) int {
	lexeme := m.lexeme
	newMode := m.rule
	resp := &Response{data: map[string]string{}}
	if newMode.beforeBegin != nil {
		newMode.beforeBegin(m, resp)
		if resp.isMatchIgnored {
			return h.doIgnore(lexeme)
		}
	}
	if newMode.onBegin != nil {
		newMode.onBegin(m, resp)
		if resp.isMatchIgnored {
			return h.doIgnore(lexeme)
		}
	}

	var endOverride *patternRegexp
	if newMode.endSameAsBegin {
		if re, err := compileEncodedPattern(escapeLiteral(lexeme), h.lang.flags); err == nil {
			endOverride = re
		}
	}

	if newMode.skip {
		h.modeBuffer += lexeme
	} else {
		if newMode.excludeBegin {
			h.modeBuffer += lexeme
		}
		h.processBuffer()
		if !newMode.returnBegin && !newMode.excludeBegin {
			h.modeBuffer = lexeme
		}
	}
	h.startNewMode(newMode, endOverride, resp.data)
	if newMode.returnBegin {
		return 0
	}
	return len(lexeme)
}

func (h *highlighter) doEndMatch(m *match) int {
	lexeme := m.lexeme
	remainder := m.input[m.index:]
	endMode := h.endOfMode(h.top, m, remainder)
	if endMode == nil {
		return noMatch
	}
	origin := h.top
	if origin.mode.skip {
		h.modeBuffer += lexeme
	} else {
		if !(origin.mode.returnEnd || origin.mode.excludeEnd) {
			h.modeBuffer += lexeme
		}
		h.processBuffer()
		if origin.mode.excludeEnd {
			h.modeBuffer = lexeme
		}
	}
	for {
		if h.top.mode.className != "" {
			h.emitter.closeNode()
		}
		if !h.top.mode.skip && len(h.top.mode.subLanguage) == 0 {
			h.relevance += float64(h.top.mode.relevance)
		}
		h.top = h.top.parent
		if h.top == endMode.parent {
			break
		}
	}
	if endMode.mode.starts != nil {
		var endOverride *patternRegexp
		if endMode.mode.starts.endSameAsBegin {
			endOverride = endMode.endRe
			if endOverride == nil {
				endOverride = endMode.mode.endRe
			}
		}
		h.startNewMode(endMode.mode.starts, endOverride, map[string]string{})
	}
	if origin.mode.returnEnd {
		return 0
	}
	return len(lexeme)
}

func (h *highlighter) processLexeme(textBefore string, m *match) int {
	var lexeme string
	if m != nil {
		lexeme = m.lexeme
	}
	h.modeBuffer += textBefore
	if m == nil {
		h.processBuffer()
		return 0
	}
	if h.lastMatch != nil && h.lastMatch.typ == ruleBegin && m.typ == ruleEnd &&
		h.lastMatch.index == m.index && lexeme == "" {
		// A zero-width begin/end collision: emit one character and advance.
		if m.index < len(m.input) {
			_, width := utf8.DecodeRuneInString(m.input[m.index:])
			h.modeBuffer += m.input[m.index : m.index+width]
			return width
		}
		return 1
	}
	h.lastMatch = m
	switch m.typ {
	case ruleBegin:
		return h.doBeginMatch(m)
	case ruleIllegal:
		if !h.ignoreIllegals {
			h.illegal = true
			return 0
		}
		if lexeme == "" {
			if m.index < len(m.input) {
				_, width := utf8.DecodeRuneInString(m.input[m.index:])
				return width
			}
			return 1
		}
		h.modeBuffer += lexeme
		return len(lexeme)
	case ruleEnd:
		if processed := h.doEndMatch(m); processed != noMatch {
			return processed
		}
		if lexeme == "" {
			// The end matched but its callback ignored it (for example a zero-width
			// `^` end guarded by a condition). Returning 0 would re-find the same
			// zero-width end forever; consume one byte instead, which is what an end
			// that simply did not match there would do.
			if m.index < len(m.input) {
				_, width := utf8.DecodeRuneInString(m.input[m.index:])
				h.modeBuffer += m.input[m.index : m.index+width]
				return width
			}
			return 0
		}
	}
	h.modeBuffer += lexeme
	return len(lexeme)
}

// floorRelevance ports the reference's Math.floor(relevance). A NaN score (a
// malformed keyword table yielding Number("abc")) becomes 0: JSON.stringify(NaN)
// is null upstream, and no in-tree language produces one.
func floorRelevance(value float64) int {
	if math.IsNaN(value) {
		return 0
	}
	return int(math.Floor(value))
}

// highlightAuto picks the registered language in subset with the highest
// relevance, approximating the reference's highlightAuto for sub-language
// detection. Like highlightAuto, plain text is always a candidate and wins a
// relevance tie because it is added first.
func highlightAuto(code string, subset []string) (*Result, string) {
	// The reference's `_processSubLanguage` passes `undefined` when the subset is
	// empty, so `subLanguage: []` (e.g. HTTP's body) auto-detects against every
	// language rather than disabling highlighting.
	if len(subset) == 0 {
		subset = autoDetectionOrder()
	}
	type candidate struct {
		result *Result
		name   string
		base   string // supersetOf, for the tie-break
	}
	// The reference unshifts the plaintext result, so it wins a tie against any
	// language that scores the same.
	candidates := []candidate{{result: &Result{Nodes: []*Node{{Text: code}}}}}
	// The reference iterates canonical names only; a subset built from
	// Languages() also contains aliases, and scoring the same language twice
	// would let its alias position settle a tie.
	seen := make(map[string]bool, len(subset))
	for _, name := range subset {
		lang, err := getCompiledLanguage(name)
		if err != nil || lang.disableAutodetect || seen[lang.name] {
			continue
		}
		seen[lang.name] = true
		// The reference's highlightAuto highlights candidates with
		// ignoreIllegals false, unlike a named subLanguage.
		candidates = append(candidates, candidate{
			result: highlightWith(lang, code, false),
			name:   lang.name,
			base:   lang.supersetOf,
		})
	}
	// Sort by relevance, then let the base language win a tie, otherwise keep
	// the original order, which is the reference's registration order (core.js
	// highlightAuto).
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.result.Relevance != right.result.Relevance {
			return left.result.Relevance > right.result.Relevance
		}
		if left.name != "" && right.name != "" {
			if left.base == right.name {
				return false
			}
			if right.base == left.name {
				return true
			}
		}
		return false
	})
	return candidates[0].result, candidates[0].name
}

func nodesEmitter(nodes []*Node) *emitter {
	root := &Node{Children: nodes}
	return &emitter{root: root, stack: []*Node{root}}
}

func startsWith(re interface{ FindStringIndex(string) []int }, s string) bool {
	loc := re.FindStringIndex(s)
	return loc != nil && loc[0] == 0
}

// lineStartsWith evaluates an assertion at the match's position with JavaScript
// line terminators, reusing this highlight's matching storage.
func (h *highlighter) lineStartsWith(re *patternRegexp, s string) bool {
	loc := re.findIndexWS(s, h.jsLines, &h.regexWS)
	return loc != nil && loc[0] == 0
}

// escapeLiteral escapes a lexeme so it matches literally (core.js escape()).
func escapeLiteral(value string) string {
	const special = `-/\^$*+?.()|[]{}`
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		if strings.IndexByte(special, value[i]) >= 0 {
			b.WriteByte('\\')
		}
		b.WriteByte(value[i])
	}
	return b.String()
}
