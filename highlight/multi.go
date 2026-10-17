package highlight

import (
	"regexp"
	"strings"
	"sync"
)

// This file ports highlight.js's MultiRegex and ResumableMultiRegex (core.js).
//
// A mode's rules are compiled into one alternation, each rule wrapped in its
// own capturing group, so a single FindStringSubmatchIndex identifies which
// rule matched. Go's regexp uses leftmost-first alternation semantics, the same
// as JavaScript, so the rule order decides ties.

type ruleType uint8

const (
	ruleBegin ruleType = iota
	ruleEnd
	ruleIllegal
)

// matchData identifies the rule a group belongs to.
type matchData struct {
	rule     *compiledMode
	typ      ruleType
	position int
}

// match is one rule match. index is the byte index into the whole source.
type match struct {
	index    int
	lexeme   string
	typ      ruleType
	rule     *compiledMode
	groups   []int
	input    string
	position int
}

type multiRegex struct {
	matchIndexes map[int]matchData
	regexes      []string
	matchAt      int
	position     int
	matcherRe    *regexp.Regexp
}

func newMultiRegex() *multiRegex {
	return &multiRegex{matchIndexes: map[int]matchData{}, matchAt: 1}
}

func (m *multiRegex) addRule(re *regexp.Regexp, data matchData) {
	data.position = m.position
	m.position++
	m.matchIndexes[m.matchAt] = data
	m.regexes = append(m.regexes, re.String())
	m.matchAt += re.NumSubexp() + 1
}

// compile builds the joined alternation. It is the port of join(), without the
// backreference renumbering the reference needs (backreferences are replaced
// per language before reaching here).
func (m *multiRegex) compile(caseInsensitive bool) error {
	if len(m.regexes) == 0 {
		return nil
	}
	var b strings.Builder
	for i, source := range m.regexes {
		if i > 0 {
			b.WriteByte('|')
		}
		b.WriteByte('(')
		b.WriteString(source)
		b.WriteByte(')')
	}
	flags := "m"
	if caseInsensitive {
		flags += "i"
	}
	re, err := compilePattern(b.String(), flags)
	if err != nil {
		return err
	}
	m.matcherRe = re
	return nil
}

// exec finds the leftmost match at or after lastIndex.
func (m *multiRegex) exec(s string, lastIndex int) *match {
	if m.matcherRe == nil || lastIndex > len(s) {
		return nil
	}
	loc := m.matcherRe.FindStringSubmatchIndex(s[lastIndex:])
	if loc == nil {
		return nil
	}
	group := 1
	for ; group < len(loc)/2; group++ {
		if loc[2*group] >= 0 {
			break
		}
	}
	data, ok := m.matchIndexes[group]
	if !ok {
		return nil
	}
	start := lastIndex + loc[0]
	end := lastIndex + loc[1]
	groups := make([]int, len(loc))
	for i, v := range loc {
		if v >= 0 {
			groups[i] = lastIndex + v
		} else {
			groups[i] = -1
		}
	}
	return &match{
		index:    start,
		lexeme:   s[start:end],
		typ:      data.typ,
		rule:     data.rule,
		groups:   groups,
		input:    s,
		position: data.position,
	}
}

// resumableMultiRegex ports ResumableMultiRegex. The lazily built matchers are
// guarded so a compiled language can be shared by concurrent highlights; the
// cursor lives in the caller's modeInstance.
type resumableMultiRegex struct {
	rules        []*regexp.Regexp
	datas        []matchData
	multiRegexes []*multiRegex
	count        int
	caseIgnore   bool
	mu           sync.Mutex
}

func (r *resumableMultiRegex) addRule(re *regexp.Regexp, data matchData) {
	r.rules = append(r.rules, re)
	r.datas = append(r.datas, data)
	if data.typ == ruleBegin {
		r.count++
	}
}

func (r *resumableMultiRegex) getMatcher(index int) *multiRegex {
	r.mu.Lock()
	defer r.mu.Unlock()
	if index < len(r.multiRegexes) && r.multiRegexes[index] != nil {
		return r.multiRegexes[index]
	}
	if index >= len(r.multiRegexes) {
		grown := make([]*multiRegex, len(r.rules))
		copy(grown, r.multiRegexes)
		r.multiRegexes = grown
	}
	matcher := newMultiRegex()
	for i := index; i < len(r.rules); i++ {
		matcher.addRule(r.rules[i], r.datas[i])
	}
	if err := matcher.compile(r.caseIgnore); err != nil {
		matcher.matcherRe = nil
	}
	r.multiRegexes[index] = matcher
	return matcher
}

// exec scans from lastIndex starting at regexIndex and returns the match and the
// next regexIndex.
func (r *resumableMultiRegex) exec(s string, lastIndex, regexIndex int) (*match, int) {
	matcher := r.getMatcher(regexIndex)
	result := matcher.exec(s, lastIndex)
	if regexIndex != 0 {
		if result == nil || result.index != lastIndex {
			full := r.getMatcher(0)
			result = full.exec(s, lastIndex+1)
		}
	}
	next := regexIndex
	if result != nil {
		next = regexIndex + result.position + 1
		if next == r.count {
			next = 0
		}
	}
	return result, next
}
