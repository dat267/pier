package highlight

import (
	"regexp/syntax"
	"strings"
	"sync"
	"unicode/utf8"
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
	// jsLines is the document's line-terminator class, already derived by the
	// highlighter, so callbacks can match a guard without rescanning the tail.
	jsLines bool
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
// lineStart marks a rule that matches only at a line start (a `^` begin):
// the dispatch tries it at line starts instead of adding its firsts to the
// candidate table, where a wide set (autohotkey's `[^\n";]`) would disable the
// dispatch for the whole rule set.
type manualRule struct {
	begin     func(s string, pos int) (end int, ok bool)
	firsts    string
	lineStart bool
	data      matchData
}

type multiRegex struct {
	matchIndexes map[int]matchData
	regexes      []string
	datas        []matchData
	matchAt      int
	position     int
	matcherRe    *patternRegexp
	flags        string
	// dispatch selects the rules that can begin at the current byte and matches
	// only those, anchored, instead of running the whole alternation's NFA.
	dispatch  bool
	ruleFirst []string
	// exacts holds each rule's parsed form for matchExactAt, or nil when the
	// walk can not decide the rule (D523); ruleGroups is each rule's group.
	exacts     []*exactNode
	ruleGroups []int
	// ruleCaps is each rule's number of capture groups.
	ruleCaps []int
	// endLineRule is the first rule that is only `$`, or -1. Its first bytes
	// are the line terminators' and it also matches at the end of the input,
	// where it is the only kind of rule that can (every other rule that can
	// match empty disables the dispatch).
	endLineRule int
	// beginLineRule is the rule that is `^` followed by an optional run (a
	// bare `^`, `^[ \t]*`), or -1, the mirror of endLineRule. It matches at
	// position 0 or just after a line terminator, which is a candidate offset
	// no first-byte set can express, so it is excluded from the first-byte
	// union and checked at line starts instead; beginLineMatch computes its
	// match with the walk or beginLineRe.
	beginLineRule int
	beginLineRe   *patternRegexp
	beginLineAtRe *patternRegexp
	// lineStartManuals is whether a manual rule is lineStart, and
	// lineStartRules whether a regex rule is led by `^`, so execDispatch
	// visits line starts for them.
	lineStartManuals bool
	lineStartRules   bool
	subs             [256]*multiRegex
	subsMu           sync.Mutex
	manuals          []manualRule
	// manualByByte indexes the manual rules by their first byte, in rule order.
	manualByByte [256][]manualRule
	// firstByteTable is the set of bytes that can begin a match, used as a byte
	// pre-filter. hasFirstBytes is false when the skip is not selective or not
	// safe (a rule can match empty). A byte table, not strings.IndexAny, because
	// IndexAny decodes its argument as UTF-8 runes and so misses a non-ASCII
	// leading byte (for example Cyrillic).
	firstByteTable [256]bool
	hasFirstBytes  bool
	firstByteCount int
	// verifyStart is set when a match can evaluate a context-sensitive
	// assertion before consuming, in which case verifyRe (the same alternation
	// prefixed with a single-char wildcard) decides whether a boundary match
	// also matches with the real preceding byte.
	verifyStart bool
	// prefixedRe is the anchored form that consumes the real preceding rune
	// before the rules, so a match at the search position is checked with the
	// correct \b/^ context without a bit-state verify pass.
	prefixedRe *patternRegexp
	// atLastIndex is set when a rule matches empty at every position (the
	// default `\B|\b` end), so the leftmost match always begins at lastIndex.
	// anchoredRe and anchoredPrefixedRe then try the alternation there only,
	// instead of scanning the rest of the input for a match.
	atLastIndex        bool
	anchoredRe         *patternRegexp
	anchoredPrefixedRe *patternRegexp
}

func newMultiRegex() *multiRegex {
	return &multiRegex{matchIndexes: map[int]matchData{}, matchAt: 1, endLineRule: -1, beginLineRule: -1}
}

func (m *multiRegex) addRule(source string, data matchData) {
	m.addRuleAt(encodeCodeUnitsString(source), m.position, data)
	m.position++
}

func (m *multiRegex) addRuleAt(source string, position int, data matchData) {
	data.position = position
	m.matchIndexes[m.matchAt] = data
	m.ruleGroups = append(m.ruleGroups, m.matchAt)
	m.ruleCaps = append(m.ruleCaps, countCapturingGroups(source))
	m.regexes = append(m.regexes, source)
	m.datas = append(m.datas, data)
	m.matchAt += countCapturingGroups(source) + 1
}

func (m *multiRegex) addManual(begin func(string, int) (int, bool), firsts string, lineStart bool, data matchData) {
	data.position = m.position
	m.position++
	m.manuals = append(m.manuals, manualRule{begin: begin, firsts: firsts, lineStart: lineStart, data: data})
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
		re, err := compileEncodedPattern(joined, flags)
		if err != nil {
			return err
		}
		m.matcherRe = re
		for _, source := range m.regexes {
			if source == `\B|\b` || strings.HasPrefix(source, `\B|\b|`) {
				anchored, err := compileEncodedPattern(`\A(?:`+joined+`)`, flags)
				prefixed, prefixedErr := compileEncodedPattern(`\A(?s:.)(?:`+joined+`)`, flags)
				if err == nil && prefixedErr == nil {
					m.atLastIndex = true
					m.anchoredRe, m.anchoredPrefixedRe = anchored, prefixed
				}
				break
			}
		}
		if parsed := parsePattern(parsedExpr); parsed != nil {
			if leadingContextSensitive(parsed) {
				m.verifyStart = true
				// The DOTALL flag is scoped to the leading wildcard: applying it to the
				// whole pattern would make every rule's `.` cross newlines, unlike the
				// reference, whose rules are compiled without `s`.
				if prefixed, err := compileEncodedPattern(`\A(?s:.)`+`(?:`+joined+`)`, flags); err == nil {
					m.prefixedRe = prefixed
				}
			}
		}
	}

	// The pre-filter's byte set: every byte that can begin a match, including the
	// manual rules'. It is disabled when a regex rule can match empty, since such
	// a rule can begin at any byte.
	m.ruleFirst = make([]string, len(m.regexes))
	m.exacts = make([]*exactNode, len(m.regexes))
	for i, source := range m.regexes {
		prepared := preparePattern(source)
		// A rule whose groups a callback reads still has a walk: its "no
		// match" is as good as the regexp's, and decideAt leaves a match to
		// the regexp, which reports the groups.
		m.exacts[i] = exactProgram(parseExactPattern(prepared, caseInsensitive))
	}
	usable := true
	var union [256]bool
	lineLed := make([]bool, len(m.regexes))
	for i, source := range m.regexes {
		set, ok := ruleFirstByteSet(preparePattern(source), caseInsensitive)
		if !ok && isEndLineRule(preparePattern(source)) {
			// `$` under the m flag: before \n, \r, U+2028 or U+2029 (lead
			// byte 0xE2), or at the end of the input. A wider set only adds
			// candidates the submatcher rejects.
			set, ok = "\n\r\xe2", true
			if m.endLineRule < 0 {
				m.endLineRule = i
			}
		}
		if !ok && isBeginLinePrefixRule(preparePattern(source)) {
			// A rule that begins at a line start and matches empty there (a
			// bare `^`, or an optional indentation run, `^[ \t]*`) sits at a
			// position no first-byte set can express, so it contributes no
			// candidate byte and is checked at line starts (D524). Without
			// this one such rule disabled the dispatch for the whole rule set:
			// markdown's root matcher and armasm's ran the joined regexp.
			if m.beginLineRule >= 0 {
				usable = false // beginLineMatch serves one such rule
				break
			}
			m.beginLineRule = i
			set, ok = "", true
		}
		if !ok {
			usable = false
			break
		}
		m.ruleFirst[i] = set
		if beginsAtLineStart(preparePattern(source)) {
			lineLed[i] = true
		}
	}
	// A rule led by `^` (basic's illegal `^.`) or a lineStart manual
	// (autohotkey's titles) can only begin at a line start, which
	// execDispatch can visit, so when the full table is past the cap their
	// bytes stay in their own submatchers but out of the candidate table:
	// `^.` alone covers nearly every byte and would disable the dispatch for
	// the whole rule set. Under the cap they stay in, since a sparse table
	// puts the next candidate far ahead and each exec call scans to it again.
	for pass := range 2 {
		union = [256]bool{}
		for i, set := range m.ruleFirst {
			if pass == 1 && lineLed[i] {
				m.lineStartRules = true
				continue
			}
			for j := range len(set) {
				union[set[j]] = true
			}
		}
		for _, mr := range m.manuals {
			if pass == 1 && mr.lineStart {
				m.lineStartManuals = true
				continue
			}
			for j := range len(mr.firsts) {
				union[mr.firsts[j]] = true
			}
		}
		count := 0
		for j := range 256 {
			if union[j] {
				count++
			}
		}
		if count <= 240 {
			break
		}
	}
	if usable && m.beginLineRule >= 0 {
		source := m.regexes[m.beginLineRule]
		plain, err := compileEncodedPattern(`\A(?:`+source+`)`, flags)
		if err != nil {
			return err
		}
		at, err := compileEncodedPattern(`\A(?s:.)(?:`+source+`)`, flags)
		if err != nil {
			return err
		}
		m.beginLineRe, m.beginLineAtRe = plain, at
	}
	if usable {
		count := 0
		for i := range 256 {
			if union[i] {
				count++
			}
		}
		lineStarts := m.beginLineRule >= 0 || m.lineStartManuals || m.lineStartRules
		if (count > 0 || lineStarts) && count <= 240 {
			// With line-start rules the table may be empty: every candidate
			// is then a line start.
			m.firstByteTable = union
			m.hasFirstBytes = true
			m.firstByteCount = count
		}
	}
	// A single rule dispatches too when the walk can settle it: execScan's
	// unanchored regexp clears a visited bitmap the size of the remaining
	// input on every call, even when the rule matches at the first byte. A
	// single rule the walk can not settle keeps the one scan, which is never
	// worse than an anchored regexp per candidate.
	single := len(m.regexes) == 1 && len(m.manuals) == 0 && m.exacts[0] != nil
	m.dispatch = usable && m.hasFirstBytes && (len(m.regexes) > 1 || len(m.manuals) > 0 || single)
	if m.dispatch {
		for _, mr := range m.manuals {
			for i := range len(mr.firsts) {
				b := mr.firsts[i]
				m.manualByByte[b] = append(m.manualByByte[b], mr)
			}
		}
	}
	return nil
}

// indexFirstByte returns the first index at or after from whose byte can begin
// a match, or -1.
func (m *multiRegex) indexFirstByte(s string, from int) int {
	if !m.hasFirstBytes || m.firstByteCount == 0 {
		return -1
	}
	for i := from; i < len(s); i++ {
		if m.firstByteTable[s[i]] {
			return i
		}
	}
	return -1
}

// subMatcher returns a matcher containing only the regex rules that can begin
// with byte b, anchored at the search position, or nil when none can.
// noSubMatcher marks a byte subMatcher found no rule for.
var noSubMatcher = &multiRegex{}

func (m *multiRegex) subMatcher(b byte) *multiRegex {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	if m.subs[b] != nil {
		if m.subs[b] == noSubMatcher {
			return nil
		}
		return m.subs[b]
	}
	sub := newMultiRegex()
	sub.flags = m.flags
	for i, source := range m.regexes {
		if strings.IndexByte(m.ruleFirst[i], b) < 0 {
			continue
		}
		sub.addRuleAt(source, m.datas[i].position, m.datas[i])
		sub.exacts = append(sub.exacts, m.exacts[i])
	}
	if len(sub.regexes) == 0 {
		// Cached too: a line start reaches bytes no rule begins with.
		m.subs[b] = noSubMatcher
		return nil
	}
	var body strings.Builder
	body.WriteString(`(?:`)
	for i, source := range sub.regexes {
		if i > 0 {
			body.WriteByte('|')
		}
		body.WriteByte('(')
		body.WriteString(source)
		body.WriteByte(')')
	}
	body.WriteByte(')')
	re, err := compileEncodedPattern(`\A`+body.String(), m.flags)
	if err != nil {
		return nil
	}
	sub.matcherRe = re
	if prefixed, err := compileEncodedPattern(`\A(?s:.)`+body.String(), m.flags); err == nil {
		sub.prefixedRe = prefixed
	}
	m.subs[b] = sub
	return sub
}

// exec finds the leftmost match at or after lastIndex.
func (m *multiRegex) exec(s string, lastIndex int, jsLines bool, ws *jsWorkspace) *match {
	if m.matcherRe == nil && len(m.manuals) == 0 {
		return nil
	}
	if lastIndex > len(s) {
		return nil
	}
	if m.atLastIndex {
		return m.execAtLastIndex(s, lastIndex, jsLines, ws)
	}
	if m.dispatch {
		return m.execDispatch(s, lastIndex, jsLines, ws)
	}
	return m.execScan(s, lastIndex, jsLines, ws)
}

// execAtLastIndex is exec for a rule set that always matches at lastIndex.
// The prefixed form consumes the real preceding rune, so `\b` and `^` are
// evaluated in context, as in execDispatch. A manual rule matching at the same
// index wins only when it was declared before the regex rule.
func (m *multiRegex) execAtLastIndex(s string, lastIndex int, jsLines bool, ws *jsWorkspace) *match {
	// The alternation is anchored here, so the exact walk can settle it rule
	// by rule in priority order before the regexp is needed (D523).
	regexMatch, decided := m.decideAt(s, lastIndex, jsLines, ws)
	if decided {
		// Manual rules still compete by position below.
	} else if lastIndex == 0 {
		if loc := m.anchoredRe.findSubmatchWS(s, jsLines, ws); loc != nil {
			regexMatch = m.buildMatch(s, 0, loc, jsLines)
		}
	} else {
		_, width := utf8.DecodeLastRuneInString(s[:lastIndex])
		base := lastIndex - width
		if loc := m.anchoredPrefixedRe.findSubmatchWS(s[base:], jsLines, ws); loc != nil {
			regexMatch = m.buildMatch(s, base, loc, jsLines)
		}
	}
	if lastIndex < len(s) {
		for i := range m.manuals {
			mr := &m.manuals[i]
			if strings.IndexByte(mr.firsts, s[lastIndex]) < 0 {
				continue
			}
			if regexMatch != nil && mr.data.position >= regexMatch.position {
				break
			}
			if end, ok := mr.begin(s, lastIndex); ok {
				return &match{
					index:    lastIndex,
					lexeme:   s[lastIndex:end],
					typ:      mr.data.typ,
					rule:     mr.data.rule,
					input:    s,
					position: mr.data.position,
					jsLines:  jsLines,
				}
			}
		}
	}
	return regexMatch
}

// maxRejectedCandidates bounds one dispatch scan. A rejected candidate that
// runs the anchored submatcher's regexp may scan the tail before failing, so a
// long run with no matching rule (e.g. repeated `0x` under JavaScript) is
// quadratic: 8 KiB took 0.4 s and 32 KiB 13.7 s. Giving up there leaves the
// remainder as plain text, the same responsiveness guard as the iteration cap
// (D351). Rejections decideAt settles without the regexp are not counted.
const maxRejectedCandidates = 256

// execAt is execDispatch's answer when it is at pos, or nil: the first match
// in rule order that begins exactly at pos.
func (m *multiRegex) execAt(s string, pos int, jsLines bool, ws *jsWorkspace) *match {
	found, _ := m.dispatchAt(s, pos, jsLines, ws)
	if pos == len(s) {
		if eom := m.endOfInputMatch(s, jsLines); eom != nil && (found == nil || eom.position < found.position) {
			found = eom
		}
	}
	if lm := m.beginLineMatch(s, pos, jsLines, ws); lm != nil && (found == nil || lm.position < found.position) {
		return lm
	}
	return found
}

func (m *multiRegex) execDispatch(s string, lastIndex int, jsLines bool, ws *jsWorkspace) *match {
	search := lastIndex
	rejected := 0
	candidate := -2
	for search <= len(s) {
		// The next position where some rule can begin: a first byte of the
		// table, or a line start for a `^` rule, which the table cannot
		// express because its match sits after the terminator's byte. A
		// candidate past a line start is kept for the next round rather than
		// scanned for again from each line start.
		if candidate < search && candidate != -1 {
			candidate = m.indexFirstByte(s, search)
		}
		lineStart := -1
		if m.beginLineRule >= 0 || m.lineStartManuals || m.lineStartRules {
			lineStart = nextLineStart(s, search, jsLines)
		}
		pos := candidate
		switch {
		case candidate < 0:
			pos = lineStart
		case lineStart >= 0 && lineStart < candidate:
			pos = lineStart
		}
		if pos < 0 {
			return m.endOfInputMatch(s, jsLines)
		}

		found, decided := m.dispatchAt(s, pos, jsLines, ws)
		// The `^` rule's match is empty, so it competes with a non-empty match
		// at the same position by rule order, as the joined alternation does.
		lm := m.beginLineMatch(s, pos, jsLines, ws)
		// The empty `$` rule matches at the end of the input; `^` can match
		// there too (after a final terminator), so both compete by order.
		if pos == len(s) {
			if eom := m.endOfInputMatch(s, jsLines); eom != nil {
				if found == nil || eom.position < found.position {
					found = eom
				}
			}
		}
		if found != nil && (lm == nil || found.position < lm.position) {
			return found
		}
		if lm != nil {
			return lm
		}
		// A rejection the walk settled costs O(1), so a run
		// of them is linear and needs no budget. Only a rejection that ran the
		// regexp, which may scan the tail, is counted (D351); otherwise a long
		// comment of words that only start like a rule (`abc` against
		// `\b(?:a|an|...)\b`) would lose its end.
		if !decided {
			rejected++
			if rejected >= maxRejectedCandidates {
				return nil
			}
		}
		search = pos + 1
	}
	return nil
}

// dispatchAt tries the rules that can begin with s[pos], returning the match
// and whether the walk settled the position without the regexp.
func (m *multiRegex) dispatchAt(s string, pos int, jsLines bool, ws *jsWorkspace) (found *match, decided bool) {
	if pos >= len(s) {
		return nil, false
	}
	b := s[pos]

	var regexMatch, manualMatch *match
	sub := m.subMatcher(b)
	if sub != nil {
		regexMatch, decided = sub.decideAt(s, pos, jsLines, ws)
	} else {
		// No regex rule begins with b: nothing ran the regexp, so a rejection
		// here is O(1) and not counted (D351). Only a line start for a `^` rule
		// or a line-start manual reaches a byte outside the table.
		decided = true
	}
	if sub != nil && !decided {
		if pos == 0 {
			if loc := sub.matcherRe.findSubmatchWS(s, jsLines, ws); loc != nil {
				regexMatch = sub.buildMatch(s, 0, loc, jsLines)
			}
		} else if sub.prefixedRe != nil {
			_, width := utf8.DecodeLastRuneInString(s[:pos])
			base := pos - width
			if loc := sub.prefixedRe.findSubmatchWS(s[base:], jsLines, ws); loc != nil {
				regexMatch = sub.buildMatch(s, base, loc, jsLines)
			}
		} else if loc := sub.matcherRe.findSubmatchWS(s[pos:], jsLines, ws); loc != nil {
			regexMatch = sub.buildMatch(s, pos, loc, jsLines)
		}
	}
	for i := range m.manualByByte[b] {
		mr := &m.manualByByte[b][i]
		if mr.lineStart && pos > 0 && !lineEndBefore(s, pos, jsLines) {
			continue
		}
		if end, ok := mr.begin(s, pos); ok {
			manualMatch = &match{
				index:    pos,
				lexeme:   s[pos:end],
				typ:      mr.data.typ,
				rule:     mr.data.rule,
				input:    s,
				position: mr.data.position,
				jsLines:  jsLines,
			}
			break
		}
	}
	switch {
	case regexMatch != nil && manualMatch != nil:
		if manualMatch.position < regexMatch.position {
			return manualMatch, decided
		}
		return regexMatch, decided
	case regexMatch != nil:
		return regexMatch, decided
	case manualMatch != nil:
		return manualMatch, decided
	}
	return nil, decided
}

// endOfInputMatch is the empty match of the first `$` rule at the end of s,
// or nil when the rule set has none.
func (m *multiRegex) endOfInputMatch(s string, jsLines bool) *match {
	if m.endLineRule < 0 {
		return nil
	}
	group := m.ruleGroups[m.endLineRule]
	data := m.matchIndexes[group]
	return &match{
		index:     len(s),
		typ:       data.typ,
		rule:      data.rule,
		input:     s,
		position:  data.position,
		groupBase: group,
		jsLines:   jsLines,
	}
}

// isEndLineRule reports whether a prepared rule is only `$` (possibly in
// groups), which the m flag makes an end-of-line assertion.
func isEndLineRule(expr string) bool {
	re := parseExactPattern(expr, false)
	for re != nil && re.Op == syntax.OpCapture {
		re = re.Sub[0]
	}
	return re != nil && re.Op == syntax.OpEndLine
}

// isBeginLinePrefixRule reports whether a prepared rule begins with a `^`
// assertion it can satisfy while matching empty, so its match starts at a line
// start whatever follows.
func isBeginLinePrefixRule(expr string) bool {
	re := parseExactPattern(expr, false)
	for re != nil && (re.Op == syntax.OpCapture || re.Op == syntax.OpConcat) {
		if len(re.Sub) == 0 {
			return false
		}
		re = re.Sub[0]
		if minMatchLen(re) > 0 {
			return false
		}
	}
	return re != nil && (re.Op == syntax.OpBeginLine || re.Op == syntax.OpBeginText)
}

// beginsAtLineStart is whether expr's leading element is `^` (the m flag's
// line start), so it can only match at a line start.
func beginsAtLineStart(expr string) bool {
	re := parseExactPattern(expr, false)
	for re != nil && (re.Op == syntax.OpCapture || re.Op == syntax.OpConcat) {
		if len(re.Sub) == 0 {
			return false
		}
		re = re.Sub[0]
	}
	return re != nil && re.Op == syntax.OpBeginLine
}

// beginLineMatch is the match of the beginLineRule at pos, or nil when the
// rule set has none, pos is not a line start, or the rule does not match.
func (m *multiRegex) beginLineMatch(s string, pos int, jsLines bool, ws *jsWorkspace) *match {
	if m.beginLineRule < 0 {
		return nil
	}
	if pos > 0 && !lineEndBefore(s, pos, jsLines) {
		return nil
	}
	// The match need not be empty, and a guard reads the text after it
	// (armasm's indented `#` comment), so the walk or the regexp gives its
	// end.
	group := m.ruleGroups[m.beginLineRule]
	data := m.matchIndexes[group]
	end := -1
	var groups []int
	if exact := m.exacts[m.beginLineRule]; exact != nil && !data.keepGroups {
		e, result := matchExactAt(exact, s, pos, jsLines, ws)
		switch result {
		case exactNoMatch:
			return nil
		case exactMatched:
			end = e
		}
	}
	if end < 0 {
		var loc []int
		base := 0
		if pos == 0 {
			loc = m.beginLineRe.findSubmatchWS(s, jsLines, ws)
		} else {
			_, width := utf8.DecodeLastRuneInString(s[:pos])
			base = pos - width
			loc = m.beginLineAtRe.findSubmatchWS(s[base:], jsLines, ws)
		}
		if loc == nil {
			return nil
		}
		end = base + loc[1]
		if data.keepGroups {
			// loc's group k is the rule's group k; the match's groups put
			// them after the wrapper group, as buildMatch's do.
			groups = make([]int, 2*group+len(loc))
			for k := range groups {
				groups[k] = -1
			}
			groups[2*group], groups[2*group+1] = pos, end
			for k := 2; k < len(loc); k++ {
				if loc[k] >= 0 {
					groups[2*group+k] = base + loc[k]
				}
			}
		}
	}
	return &match{
		index:     pos,
		lexeme:    s[pos:end],
		typ:       data.typ,
		rule:      data.rule,
		groups:    groups,
		input:     s,
		position:  data.position,
		groupBase: group,
		jsLines:   jsLines,
	}
}

// nextLineStart returns the first position at or after from that `^` can match
// at (0 or just after a line terminator), or -1 when there is none.
func nextLineStart(s string, from int, jsLines bool) int {
	if from <= 0 {
		return 0
	}
	if from <= len(s) && lineEndBefore(s, from, jsLines) {
		return from
	}
	for i := from; i < len(s); i++ {
		if s[i] == '\n' {
			return i + 1
		}
		if !jsLines {
			continue
		}
		if s[i] == '\r' {
			return i + 1
		}
		if s[i] == 0xE2 {
			if r, width := utf8.DecodeRuneInString(s[i:]); width > 0 && (r == '\u2028' || r == '\u2029') {
				return i + width
			}
		}
	}
	return -1
}

// execScan is the joined-alternation path used when per-byte dispatch is not
// available. Manual rules are tested position by position against the leftmost
// regex match.
func (m *multiRegex) execScan(s string, lastIndex int, jsLines bool, ws *jsWorkspace) *match {
	search := lastIndex
	for search <= len(s) {
		regexAbs := -1
		var regexLoc []int
		if m.matcherRe != nil {
			if m.hasFirstBytes && !m.lineStartManuals && !m.lineStartRules && m.beginLineRule < 0 {
				// The table leaves out line-start rules (execDispatch visits
				// line starts for them), so it is no filter for the scan then.
				candidate := m.indexFirstByte(s, search)
				if candidate < 0 {
					return m.endOfInputMatch(s, jsLines)
				}
				search = candidate
			}
			regexLoc = m.matcherRe.findSubmatchWS(s[search:], jsLines, ws)
			if regexLoc != nil {
				regexAbs = search + regexLoc[0]
			}
		}
		manualAbs, manualEnd := -1, 0
		var manualHit *manualRule
		// With no manual rules the scan below only walks to regexAbs or the end
		// of the input doing nothing, which is O(tail) per call: execScan calls
		// it once per candidate (D536).
		for p := search; len(m.manuals) > 0 && p < len(s); p++ {
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
		var regexMatch, manualMatch *match
		if manualAbs >= 0 {
			manualMatch = &match{
				index:    manualAbs,
				lexeme:   s[manualAbs:manualEnd],
				typ:      manualHit.data.typ,
				rule:     manualHit.data.rule,
				input:    s,
				position: manualHit.data.position,
				jsLines:  jsLines,
			}
		}
		if regexAbs >= 0 {
			if !m.verifyStart || regexAbs == 0 {
				regexMatch = m.buildMatch(s, search, regexLoc, jsLines)
			} else if m.prefixedRe != nil {
				// Rebuild the match from the prefixed result: the leading wildcard
				// consumes the real preceding byte, so `^`/`\b` are evaluated in
				// context and the correct rule wins. Returning the slice match
				// instead would keep a rule that only matched because the slice
				// boundary looked like a line start.
				_, width := utf8.DecodeLastRuneInString(s[:regexAbs])
				base := regexAbs - width
				if vloc := m.prefixedRe.findSubmatchWS(s[base:], jsLines, ws); vloc != nil && vloc[0] == 0 {
					regexMatch = m.buildMatch(s, base, vloc, jsLines)
				}
			}
			if regexMatch == nil {
				search = regexAbs + 1
				continue
			}
		}
		// Rules are evaluated in declaration order, so at the same position the
		// earlier rule wins; a manual rule stands in for its own regex rule and
		// must not preempt an earlier one.
		switch {
		case regexMatch != nil && manualMatch != nil:
			if manualMatch.index < regexMatch.index {
				return manualMatch
			}
			if manualMatch.index > regexMatch.index || manualMatch.position >= regexMatch.position {
				return regexMatch
			}
			return manualMatch
		case manualMatch != nil:
			return manualMatch
		case regexMatch != nil:
			return regexMatch
		}
		return nil
	}
	return nil
}

// decideAt settles a submatcher at pos without its regexp when it can: rules
// are tried in priority order by the exact walk (D523), and the first that
// matches wins, as it does in the anchored alternation. decided is false at
// the first rule the walk cannot settle, and the caller then runs the regexp.
// The walk is bounded per rule (exactBudget steps), so a decision costs O(1)
// whatever the input's length.
func (m *multiRegex) decideAt(s string, pos int, jsLines bool, ws *jsWorkspace) (found *match, decided bool) {
	for i, exact := range m.exacts {
		if exact != nil {
			end, result := matchExactAt(exact, s, pos, jsLines, ws)
			switch result {
			case exactMatched:
				data := m.matchIndexes[m.ruleGroups[i]]
				var groups []int
				if data.keepGroups {
					// A callback reads the groups: walk again recording them,
					// laid out as the regexp's submatches are, the rule's own
					// groups following its wrapper group.
					group := m.ruleGroups[i]
					groups = make([]int, 2*(group+m.ruleCaps[i]+1))
					for k := range groups {
						groups[k] = -1
					}
					caps := groups[2*group:]
					if again, result := matchExactCaps(exact, s, pos, jsLines, ws, caps); result != exactMatched || again != end {
						return nil, false
					}
					caps[0], caps[1] = pos, end
				}
				return &match{
					index:     pos,
					lexeme:    s[pos:end],
					typ:       data.typ,
					rule:      data.rule,
					groups:    groups,
					input:     s,
					position:  data.position,
					groupBase: m.ruleGroups[i],
					jsLines:   jsLines,
				}, true
			case exactNoMatch:
				continue
			}
		}
		return nil, false
	}
	return nil, true
}

// buildMatch builds a match from a submatch result relative to base.
func (m *multiRegex) buildMatch(s string, base int, loc []int, jsLines bool) *match {
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
	start := base + loc[2*group]
	end := base + loc[2*group+1]
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
		jsLines:   jsLines,
	}
}

type subRule struct {
	src       string
	keep      bool
	manual    func(string, int) (int, bool)
	firsts    string
	lineStart bool
	data      matchData
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

func (r *resumableMultiRegex) addManual(begin func(string, int) (int, bool), firsts string, lineStart bool, data matchData) {
	r.rules = append(r.rules, subRule{manual: begin, firsts: firsts, lineStart: lineStart, data: data})
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
		// The reference stores matchers in a sparse JS array, so an index at (or
		// past) rules.length is valid: rules.slice(index) is empty and the empty
		// matcher matches nothing. regexIndex can reach rules.length when the last
		// rule's position advances past count, so size to index+1, not rules
		// length alone.
		size := max(index+1, len(r.rules))
		grown := make([]*multiRegex, size)
		copy(grown, r.multiRegexes)
		r.multiRegexes = grown
	}
	matcher := newMultiRegex()
	for i := index; i < len(r.rules); i++ {
		rule := r.rules[i]
		if rule.manual != nil {
			matcher.addManual(rule.manual, rule.firsts, rule.lineStart, rule.data)
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
func (r *resumableMultiRegex) exec(s string, lastIndex, regexIndex int, jsLines bool, ws *jsWorkspace) (*match, int) {
	matcher := r.getMatcher(regexIndex)
	var result *match
	if regexIndex != 0 && matcher.dispatch && !matcher.atLastIndex && lastIndex <= len(s) {
		// The reference keeps the resumed matcher's result only when it is
		// at lastIndex, and otherwise runs the full matcher from
		// lastIndex+1, so a scan past lastIndex is wasted. Over a long tail
		// of candidates that made every resume O(tail) (R, and HTTP's
		// auto-detected body, D536); the match at lastIndex alone is the
		// same answer.
		result = matcher.execAt(s, lastIndex, jsLines, ws)
	} else {
		result = matcher.exec(s, lastIndex, jsLines, ws)
	}
	base := regexIndex
	if regexIndex != 0 {
		if result == nil || result.index != lastIndex {
			full := r.getMatcher(0)
			result = full.exec(s, lastIndex+1, jsLines, ws)
			// The fallback result comes from the full matcher, so its rule position
			// is already absolute. Adding the partial index to it (what the
			// reference does, relying on its shared rule objects) overshoots and
			// skips the rules between, so base from zero.
			base = 0
		}
	}
	next := regexIndex
	if result != nil {
		next = base + result.position + 1
		if next == r.count {
			next = 0
		}
	}
	return result, next
}
