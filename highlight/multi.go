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
// as JavaScript, so the rule order decides ties. Rules RE2 cannot express
// (backreferences) are registered as manual rules that take part in the same
// ordering.

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
	index     int
	lexeme    string
	typ       ruleType
	rule      *compiledMode
	groups    []int
	input     string
	position  int
	groupBase int
}

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

// manualRule is a begin rule with a Go matcher instead of a regex. firsts is
// the set of bytes a match can start with, for the per-byte dispatch.
type manualRule struct {
	begin  func(s string, pos int) (end int, ok bool)
	firsts string
	data   matchData
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
	manuals   []manualRule
	// manualByByte indexes the manual rules by their first byte, in rule order.
	manualByByte [256][]manualRule
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

func (m *multiRegex) addManual(begin func(string, int) (int, bool), firsts string, data matchData) {
	data.position = m.position
	m.position++
	m.manuals = append(m.manuals, manualRule{begin: begin, firsts: firsts, data: data})
}

// compile builds the joined alternation. It is the port of join(), without the
// backreference renumbering the reference needs (backreferences are replaced
// per language before reaching here).
func (m *multiRegex) compile(caseInsensitive bool) error {
	flags := "m"
	if caseInsensitive {
		flags += "i"
	}
	m.flags = flags

	var joined string
	var parsedExpr string
	if len(m.regexes) > 0 {
		var b strings.Builder
		for i, source := range m.regexes {
			if i > 0 {
				b.WriteByte('|')
			}
			b.WriteByte('(')
			b.WriteString(source)
			b.WriteByte(')')
		}
		joined = b.String()
		parsedExpr = "(?" + flags + ")" + joined
		re, err := compilePattern(joined, flags)
		if err != nil {
			return err
		}
		m.matcherRe = re
		if parsed := parsePattern(parsedExpr); parsed != nil {
			if leadingContextSensitive(parsed) {
				m.verifyStart = true
				verifyRe, err := compilePattern("(?s).(?:"+joined+")", flags)
				if err == nil {
					m.verifyRe = verifyRe
				}
			}
			m.firstBytes, _ = firstByteSet(parsedExpr, parsed)
		}
	}

	// Per-byte dispatch is usable when every regex rule always consumes at least
	// one byte and its first-byte set is known.
	m.ruleFirst = make([]string, len(m.regexes))
	usable := true
	var union [256]bool
	for i, source := range m.regexes {
		set, ok := ruleFirstByteSet(source)
		if !ok {
			usable = false
			break
		}
		m.ruleFirst[i] = set
		for j := 0; j < len(set); j++ {
			union[set[j]] = true
		}
	}
	for _, mr := range m.manuals {
		for i := 0; i < len(mr.firsts); i++ {
			union[mr.firsts[i]] = true
		}
	}
	m.dispatch = usable && (len(m.regexes) > 1 || len(m.manuals) > 0)
	if m.dispatch {
		for _, mr := range m.manuals {
			for i := 0; i < len(mr.firsts); i++ {
				b := mr.firsts[i]
				m.manualByByte[b] = append(m.manualByByte[b], mr)
			}
		}
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

// subMatcher returns a matcher containing only the regex rules that can begin
// with byte b, anchored at the search position, or nil when none can.
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
	m.subs[b] = sub
	return sub
}

// exec finds the leftmost match at or after lastIndex.
func (m *multiRegex) exec(s string, lastIndex int) *match {
	if m.matcherRe == nil && len(m.manuals) == 0 {
		return nil
	}
	if lastIndex > len(s) {
		return nil
	}
	if m.dispatch {
		return m.execDispatch(s, lastIndex)
	}
	return m.execScan(s, lastIndex)
}

func (m *multiRegex) execDispatch(s string, lastIndex int) *match {
	search := lastIndex
	for search <= len(s) {
		candidate := strings.IndexAny(s[search:], m.firstBytes)
		if candidate < 0 {
			return nil
		}
		search += candidate
		b := s[search]

		var regexMatch, manualMatch *match
		if sub := m.subMatcher(b); sub != nil {
			if loc := sub.matcherRe.FindStringSubmatchIndex(s[search:]); loc != nil {
				if !m.verifyStart || search == 0 {
					regexMatch = sub.buildMatch(s, search, loc)
				} else if m.verifyRe != nil {
					if vloc := m.verifyRe.FindStringIndex(s[search-1:]); vloc != nil && vloc[0] == 0 {
						if full := m.matcherRe.FindStringSubmatchIndex(s[search:]); full != nil && full[0] == 0 {
							regexMatch = m.buildMatch(s, search, full)
						}
					}
				}
			}
		}
		for i := range m.manualByByte[b] {
			mr := &m.manualByByte[b][i]
			if end, ok := mr.begin(s, search); ok {
				manualMatch = &match{
					index:    search,
					lexeme:   s[search:end],
					typ:      mr.data.typ,
					rule:     mr.data.rule,
					input:    s,
					position: mr.data.position,
				}
				break
			}
		}
		switch {
		case regexMatch != nil && manualMatch != nil:
			if manualMatch.position < regexMatch.position {
				return manualMatch
			}
			return regexMatch
		case regexMatch != nil:
			return regexMatch
		case manualMatch != nil:
			return manualMatch
		}
		search++
	}
	return nil
}

// execScan is the joined-alternation path used when per-byte dispatch is not
// available. Manual rules are tested position by position against the leftmost
// regex match.
func (m *multiRegex) execScan(s string, lastIndex int) *match {
	search := lastIndex
	for search <= len(s) {
		regexAbs := -1
		var regexLoc []int
		if m.matcherRe != nil {
			if m.firstBytes != "" {
				candidate := strings.IndexAny(s[search:], m.firstBytes)
				if candidate < 0 && len(m.manuals) == 0 {
					return nil
				}
				search += candidate
			}
			regexLoc = m.matcherRe.FindStringSubmatchIndex(s[search:])
			if regexLoc != nil {
				regexAbs = search + regexLoc[0]
			}
		}
		manualAbs, manualEnd := -1, 0
		var manualHit *manualRule
		for p := search; p < len(s); p++ {
			if regexAbs >= 0 && p > regexAbs {
				break
			}
			for i := range m.manuals {
				mr := &m.manuals[i]
				if strings.IndexByte(mr.firsts, s[p]) < 0 {
					continue
				}
				if end, ok := mr.begin(s, p); ok {
					manualAbs, manualEnd, manualHit = p, end, mr
					break
				}
			}
			if manualAbs >= 0 {
				break
			}
		}
		if manualAbs >= 0 && (regexAbs < 0 || manualAbs <= regexAbs) {
			return &match{
				index:    manualAbs,
				lexeme:   s[manualAbs:manualEnd],
				typ:      manualHit.data.typ,
				rule:     manualHit.data.rule,
				input:    s,
				position: manualHit.data.position,
			}
		}
		if regexAbs >= 0 {
			if !m.verifyStart || regexAbs == search && search == 0 {
				return m.buildMatch(s, search, regexLoc)
			}
			if m.verifyRe != nil && regexAbs > 0 {
				if vloc := m.verifyRe.FindStringIndex(s[regexAbs-1:]); vloc != nil && vloc[0] == 0 {
					return m.buildMatch(s, search, regexLoc)
				}
				search = regexAbs + 1
				continue
			}
			return m.buildMatch(s, search, regexLoc)
		}
		return nil
	}
	return nil
}

// buildMatch builds a match from a submatch result relative to base.
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

type subRule struct {
	src    string
	keep   bool
	manual func(string, int) (int, bool)
	firsts string
	data   matchData
}

// resumableMultiRegex ports ResumableMultiRegex. The lazily built matchers are
// guarded so a compiled language can be shared by concurrent highlights; the
// cursor lives in the caller's modeInstance.
type resumableMultiRegex struct {
	rules        []subRule
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
	r.rules = append(r.rules, subRule{src: source, keep: keepGroups, data: data})
	if data.typ == ruleBegin {
		r.count++
	}
}

func (r *resumableMultiRegex) addManual(begin func(string, int) (int, bool), firsts string, data matchData) {
	r.rules = append(r.rules, subRule{manual: begin, firsts: firsts, data: data})
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
		rule := r.rules[i]
		if rule.manual != nil {
			matcher.addManual(rule.manual, rule.firsts, rule.data)
		} else {
			matcher.addRule(rule.src, rule.data)
		}
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
