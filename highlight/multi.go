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
	rule       *compiledMode
	typ        ruleType
	position   int
	keepGroups bool
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
	// firstBytes is the set of bytes that can begin a match (an IndexAny
	// argument); empty when the skip is not selective or not safe.
	firstBytes string
	// verifyStart is set when a match can evaluate a context-sensitive
	// assertion before consuming, in which case verifyRe (the same alternation
	// prefixed with a single-char wildcard) decides whether a boundary match
	// also matches with the real preceding byte.
	verifyStart bool
	verifyRe    *regexp.Regexp
}

func newMultiRegex() *multiRegex {
	return &multiRegex{matchIndexes: map[int]matchData{}, matchAt: 1}
}

func (m *multiRegex) addRule(source string, data matchData) {
	data.position = m.position
	m.position++
	m.matchIndexes[m.matchAt] = data
	m.regexes = append(m.regexes, source)
	m.matchAt += countCapturingGroups(source) + 1
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
	expr := "(?" + flags + ")" + b.String()
	re, err := compilePattern(b.String(), flags)
	if err != nil {
		return err
	}
	m.matcherRe = re
	if parsed := parsePattern(expr); parsed != nil && leadingContextSensitive(parsed) {
		m.verifyStart = true
		m.firstBytes, _ = firstByteSet(expr, parsed)
		verifyRe, err := compilePattern("(?s).(?:"+b.String()+")", flags)
		if err == nil {
			m.verifyRe = verifyRe
		}
	}
	if m.verifyRe == nil && m.firstBytes == "" {
		if parsed := parsePattern(expr); parsed != nil {
			m.firstBytes, _ = firstByteSet(expr, parsed)
		}
	}
	return nil
}

// exec finds the leftmost match at or after lastIndex.
func (m *multiRegex) exec(s string, lastIndex int) *match {
	if m.matcherRe == nil || lastIndex > len(s) {
		return nil
	}
	search := lastIndex
	for {
		if m.firstBytes != "" {
			candidate := strings.IndexAny(s[search:], m.firstBytes)
			if candidate < 0 {
				return nil
			}
			search += candidate
		}
		loc := m.matcherRe.FindStringSubmatchIndex(s[search:])
		if loc == nil {
			return nil
		}
		if loc[0] != 0 || !m.verifyStart || search == 0 {
			return m.buildMatch(s, search, loc)
		}
		// The match begins exactly at the slice boundary, where the regexp sees
		// a text start that may not exist. verifyRe consumes the real preceding
		// byte and then the same alternation, so a match at relative 0 proves
		// the boundary match is also a match in context.
		if m.verifyRe != nil {
			if vloc := m.verifyRe.FindStringIndex(s[search-1:]); vloc != nil && vloc[0] == 0 {
				return m.buildMatch(s, search, loc)
			}
		}
		search++
		if search > len(s) {
			return nil
		}
	}
}

// matchAt builds a match from a submatch result relative to base.
func (m *multiRegex) buildMatch(s string, base int, loc []int) *match {
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
	start := base + loc[0]
	end := base + loc[1]
	var groups []int
	if data.keepGroups {
		groups = make([]int, len(loc))
		for i, v := range loc {
			if v >= 0 {
				groups[i] = base + v
			} else {
				groups[i] = -1
			}
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
	sources      []string
	datas        []matchData
	multiRegexes []*multiRegex
	count        int
	caseIgnore   bool
	mu           sync.Mutex
}

func (r *resumableMultiRegex) addRule(source string, keepGroups bool, data matchData) {
	if !keepGroups {
		source = stripCapturingGroups(source)
	}
	data.keepGroups = keepGroups
	r.sources = append(r.sources, source)
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
		grown := make([]*multiRegex, len(r.sources))
		copy(grown, r.multiRegexes)
		r.multiRegexes = grown
	}
	matcher := newMultiRegex()
	for i := index; i < len(r.sources); i++ {
		matcher.addRule(r.sources[i], r.datas[i])
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
