package highlight

import (
	"regexp"
	"strings"
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
}

type modeInstance struct {
	mode       *compiledMode
	parent     *modeInstance
	endRe      *regexp.Regexp
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
	relevance        int
	index            int
	iterations       int
	resumeScanAtSame bool
	lastMatch        *match
	continuations    map[string]*compiledMode
}

const noMatch = -1

func highlightWith(lang *compiledLanguage, code string) *Result {
	h := &highlighter{
		lang:          lang,
		rootMode:      lang.root,
		emitter:       newEmitter(),
		continuations: map[string]*compiledMode{},
	}
	h.top = &modeInstance{mode: lang.root, data: map[string]string{}}
	h.processContinuations()
	h.top.regexIndex = 0

	for {
		h.iterations++
		if h.resumeScanAtSame {
			h.resumeScanAtSame = false
		} else {
			h.top.regexIndex = 0
		}
		m, next := h.top.mode.matcher.exec(code, h.index, h.top.regexIndex)
		h.top.regexIndex = next
		if m == nil {
			break
		}
		before := code[h.index:m.index]
		processed := h.processLexeme(before, m)
		h.index = m.index + processed
		if h.index > len(code) {
			// A zero-width match at the end advances one past it; JavaScript's
			// substr clamps, Go slicing does not.
			h.index = len(code)
		}
		if h.iterations > 1000000 {
			break
		}
	}
	h.processLexeme(code[h.index:], nil)
	h.emitter.closeAllNodes()

	return &Result{
		Language:  lang.name,
		Relevance: h.relevance,
		Nodes:     h.emitter.root.Children,
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

func (h *highlighter) processSubLanguage() {
	if h.modeBuffer == "" {
		return
	}
	if len(h.top.mode.subLanguage) != 1 {
		// Auto-detection over a subset is not ported; emit the content plainly.
		h.emitter.addText(h.modeBuffer)
		return
	}
	name := h.top.mode.subLanguage[0]
	sub := compiledLanguages[name]
	if sub == nil {
		h.emitter.addText(h.modeBuffer)
		return
	}
	result := highlightWith(sub, h.modeBuffer)
	if h.top.mode.relevance > 0 {
		h.relevance += result.Relevance
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

func (h *highlighter) startNewMode(mode *compiledMode, endRe *regexp.Regexp, data map[string]string) {
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
	matched := endRe != nil && startsWith(endRe, remainder)
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
	if h.top.regexIndex == 0 {
		if lexeme != "" {
			h.modeBuffer += lexeme[:1]
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

	var endOverride *regexp.Regexp
	if newMode.endSameAsBegin {
		if re, err := compilePattern(escapeLiteral(lexeme), h.lang.flags); err == nil {
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
			h.relevance += h.top.mode.relevance
		}
		h.top = h.top.parent
		if h.top == endMode.parent {
			break
		}
	}
	if endMode.mode.starts != nil {
		var endOverride *regexp.Regexp
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
			h.modeBuffer += m.input[m.index : m.index+1]
		}
		return 1
	}
	h.lastMatch = m
	switch m.typ {
	case ruleBegin:
		return h.doBeginMatch(m)
	case ruleIllegal:
		if lexeme == "" {
			return 1
		}
		h.modeBuffer += lexeme
		return len(lexeme)
	case ruleEnd:
		if processed := h.doEndMatch(m); processed != noMatch {
			return processed
		}
	}
	h.modeBuffer += lexeme
	return len(lexeme)
}

func nodesEmitter(nodes []*Node) *emitter {
	root := &Node{Children: nodes}
	return &emitter{root: root, stack: []*Node{root}}
}

func startsWith(re *regexp.Regexp, s string) bool {
	loc := re.FindStringIndex(s)
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
