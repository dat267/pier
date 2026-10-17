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
// group returns the matched rule's capture group i, or "" when it did not
// participate. The rule's groups follow its wrapper group in the joined
// alternation, so the index is offset by groupBase.
func (m *match) group(i int) string {
	index := m.groupBase + i
	if 2*index+1 >= len(m.groups) || m.groups[2*index] < 0 || m.groups[2*index+1] < 0 {
		return ""
	}
	return m.input[m.groups[2*index]:m.groups[2*index+1]]
}

type match struct {
	index     int
	lexeme    string
	typ       ruleType
	rule      *compiledMode
	groups    []int
	input     string
	position  int
	groupBase int
}

type multiRegex struct {
	matchIndexes map[int]matchData
	regexes      []string
	datas        []matchData
	matchAt      int
	position     int
	matcherRe    *regexp.Regexp
	flags        string
	// dispatch selects the rules that can begin at the current byte and matches
	// only those, anchored, instead of running the whole alternation's NFA.
	dispatch  bool
	ruleFirst []string
	subs      [256]*multiRegex
	subsMu    sync.Mutex
	// single is set when the matcher has exactly one rule whose callbacks do
	// not read capture groups, so a match can be recovered with
	// FindStringIndex instead of submatch extraction.
	single bool
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
	m.addRuleAt(source, m.position, data)
	m.position++
}

func (m *multiRegex) addRuleAt(source string, position int, data matchData) {
	data.position = position
	m.matchIndexes[m.matchAt] = data
	m.regexes = append(m.regexes, source)
	m.datas = append(m.datas, data)
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
	m.flags = flags
	expr := "(?" + flags + ")" + b.String()
	re, err := compilePattern(b.String(), flags)
	if err != nil {
		return err
	}
	m.matcherRe = re
	if parsed := parsePattern(expr); parsed != nil {
		if leadingContextSensitive(parsed) {
			m.verifyStart = true
			m.firstBytes, _ = firstByteSet(expr, parsed)
			verifyRe, err := compilePattern("(?s).(?:"+b.String()+")", flags)
			if err == nil {
				m.verifyRe = verifyRe
			}
		}
		if m.firstBytes == "" {
			m.firstBytes, _ = firstByteSet(expr, parsed)
		}
	}
	// Per-byte dispatch is usable when every rule always consumes at least one
	// byte and its first-byte set is known.
	m.dispatch = len(m.regexes) > 1
	m.ruleFirst = make([]string, len(m.regexes))
	var union [256]bool
	for i, source := range m.regexes {
		set, usable := ruleFirstByteSet(source)
		if !usable {
			m.dispatch = false
			break
		}
		m.ruleFirst[i] = set
		for j := 0; j < len(set); j++ {
			union[set[j]] = true
		}
	}
	if m.dispatch {
		var ub strings.Builder
		for i := 0; i < 256; i++ {
			if union[i] {
				ub.WriteByte(byte(i))
			}
		}
		m.firstBytes = ub.String()
		if len(m.firstBytes) > 240 {
			m.dispatch = false
		}
	}
	return nil
}

// subMatcher returns a matcher containing only the rules that can begin with
// byte b, anchored at the search position, or nil when none can.
func (m *multiRegex) subMatcher(b byte) *multiRegex {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	if m.subs[b] != nil {
		return m.subs[b]
	}
	sub := newMultiRegex()
	sub.flags = m.flags
	for i, source := range m.regexes {
		if strings.IndexByte(m.ruleFirst[i], b) < 0 {
			continue
		}
		sub.addRuleAt(source, m.datas[i].position, m.datas[i])
	}
	if len(sub.regexes) == 0 {
		return nil
	}
	var pattern strings.Builder
	pattern.WriteString(`\A(?:`)
	for i, source := range sub.regexes {
		if i > 0 {
			pattern.WriteByte('|')
		}
		pattern.WriteByte('(')
		pattern.WriteString(source)
		pattern.WriteByte(')')
	}
	pattern.WriteByte(')')
	re, err := compilePattern(pattern.String(), m.flags)
	if err != nil {
		return nil
	}
	sub.matcherRe = re
	sub.single = len(sub.regexes) == 1 && !sub.datas[0].keepGroups
	m.subs[b] = sub
	return sub
}

// exec finds the leftmost match at or after lastIndex.
func (m *multiRegex) exec(s string, lastIndex int) *match {
	if m.matcherRe == nil || lastIndex > len(s) {
		return nil
	}
	if m.dispatch {
		search := lastIndex
		for search <= len(s) {
			candidate := strings.IndexAny(s[search:], m.firstBytes)
			if candidate < 0 {
				return nil
			}
			search += candidate
			sub := m.subMatcher(s[search])
			if sub == nil {
				search++
				continue
			}
			var loc []int
			if sub.single {
				loc = sub.matcherRe.FindStringIndex(s[search:])
			} else {
				loc = sub.matcherRe.FindStringSubmatchIndex(s[search:])
			}
			if loc == nil {
				search++
				continue
			}
			if !m.verifyStart || search == 0 {
				return sub.buildMatch(s, search, loc)
			}
			if m.verifyRe != nil {
				if vloc := m.verifyRe.FindStringIndex(s[search-1:]); vloc != nil && vloc[0] == 0 {
					if full := m.matcherRe.FindStringSubmatchIndex(s[search:]); full != nil && full[0] == 0 {
						return m.buildMatch(s, search, full)
					}
				}
			}
			search++
		}
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
		index:     start,
		lexeme:    s[start:end],
		typ:       data.typ,
		rule:      data.rule,
		groups:    groups,
		input:     s,
		position:  data.position,
		groupBase: group,
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
