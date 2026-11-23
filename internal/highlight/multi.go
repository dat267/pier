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
	matcherRe    *patternRegexp
	flags        string
	// dispatch selects the rules that can begin at the current byte and matches
	// only those, anchored, instead of running the whole alternation's NFA.
	dispatch  bool
	ruleFirst []string
	// prefilters holds each rule's parsed form for mayMatchAt; nil admits all.
	prefilters []*syntax.Regexp
	// exacts holds each rule's parsed form for matchExactAt, or nil when the
	// walk can not decide the rule (D523); ruleGroups is each rule's group.
	exacts     []*exactNode
	ruleGroups []int
	// requiredLits is, per rule, a literal every match must contain, or "".
	requiredLits []string
	// endLineRule is the first rule that is only `$`, or -1. Its first bytes
	// are the line terminators' and it also matches at the end of the input,
	// where it is the only kind of rule that can (every other rule that can
	// match empty disables the dispatch).
	endLineRule int
	// beginLineRule is the first rule that is only `^`, or -1, the mirror of
	// endLineRule. It matches at position 0 or just after a line terminator,
	// which is a candidate offset no first-byte set can express, so it is
	// excluded from the first-byte union and checked at line starts instead.
	beginLineRule int
	subs          [256]*multiRegex
	subsMu        sync.Mutex
	manuals       []manualRule
	// manualByByte indexes the manual rules by their first byte, in rule order.
	manualByByte [256][]manualRule
	// firstByteTable is the set of bytes that can begin a match, used as a byte
	// pre-filter. hasFirstBytes is false when the skip is not selective or not
	// safe (a rule can match empty). A byte table, not strings.IndexAny, because
	// IndexAny decodes its argument as UTF-8 runes and so misses a non-ASCII
	// leading byte (for example Cyrillic).
	firstByteTable [256]bool
	hasFirstBytes  bool
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
	m.prefilters = make([]*syntax.Regexp, len(m.regexes))
	m.exacts = make([]*exactNode, len(m.regexes))
	m.requiredLits = make([]string, len(m.regexes))
	for i, source := range m.regexes {
		prepared := preparePattern(source)
		m.prefilters[i] = rulePrefilter(prepared, caseInsensitive)
		m.requiredLits[i] = requiredLiteral(parsePatternFlags(prepared, caseInsensitive))
		// A rule whose groups a callback reads still has a walk: its "no
		// match" is as good as the regexp's, and decideAt leaves a match to
		// the regexp, which reports the groups.
		m.exacts[i] = exactProgram(parseExactPattern(prepared, caseInsensitive))
	}
	usable := true
	var union [256]bool
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
			// A rule that begins at a line start and matches empty there (an
			// optional indentation run, `^[ \t]*`) is the same shape as a bare
			// `^` (D524): its match sits at a line start, which no first-byte
			// set of the rule's own first character can express, so it is
			// checked at line starts and contributes no candidate byte. Without
			// this one such rule disabled the dispatch for the whole rule set,
			// which left armasm on the joined regexp for 81% of its profile.
			if m.beginLineRule < 0 {
				m.beginLineRule = i
			}
			set, ok = "", true
		}
		if !ok && isBeginLineRule(preparePattern(source)) {
			// `^` under the m flag matches at position 0 or after a line
			// terminator. That is the position of the byte *after* a terminator,
			// which a first-byte set of the terminator itself cannot express, so
			// the rule contributes no candidate byte and is checked at line
			// starts in execDispatch. Without this it had no first-byte set at
			// all, which disabled the dispatch for every rule set containing one:
			// markdown's root matcher paid a joined NFA scan per node instead.
			if m.beginLineRule < 0 {
				m.beginLineRule = i
			}
			set, ok = "", true
		}
		if !ok {
			usable = false
			break
		}
		m.ruleFirst[i] = set
		for j := range len(set) {
			union[set[j]] = true
		}
	}
	for _, mr := range m.manuals {
		for i := range len(mr.firsts) {
			union[mr.firsts[i]] = true
		}
	}
	if usable {
		count := 0
		for i := range 256 {
			if union[i] {
				count++
			}
		}
		if count > 0 && count <= 240 {
			m.firstByteTable = union
			m.hasFirstBytes = true
		}
	}
	m.dispatch = usable && m.hasFirstBytes && (len(m.regexes) > 1 || len(m.manuals) > 0)
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
	if !m.hasFirstBytes {
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
		sub.prefilters = append(sub.prefilters, m.prefilters[i])
		sub.exacts = append(sub.exacts, m.exacts[i])
		sub.requiredLits = append(sub.requiredLits, m.requiredLits[i])
	}
	if len(sub.regexes) == 0 {
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
	if ws != nil {
		// A mask is only valid for the tail it was computed from.
		ws.maskEpoch++
	}
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

func (m *multiRegex) execDispatch(s string, lastIndex int, jsLines bool, ws *jsWorkspace) *match {
	search := lastIndex
	rejected := 0
	for search <= len(s) {
		// The next position where some rule can begin: a first byte of the
		// table, or a line start for a `^` rule, which the table cannot
		// express because its match sits after the terminator's byte.
		candidate := m.indexFirstByte(s, search)
		lineStart := -1
		if m.beginLineRule >= 0 {
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
		if found == nil && decided && !m.anyRulePossible(s, pos, ws) {
			// No rule can match anywhere in the remainder, so the rest of the
			// scan finds nothing: on R's tail rule set the only reachable rule
			// needs `<-`, absent from the input, and the dispatch otherwise
			// stepped through 73,625 candidates per highlight.
			return m.endOfInputMatch(s, jsLines)
		}
		// The `^` rule's match is empty, so it competes with a non-empty match
		// at the same position by rule order, as the joined alternation does.
		lm := m.beginLineMatch(s, pos, jsLines)
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
		// A rejection the walk or the prefilter settled costs O(1), so a run
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

// anyRulePossible reports whether any rule of this matcher could match at or
// after pos.
func (m *multiRegex) anyRulePossible(s string, pos int, ws *jsWorkspace) bool {
	if len(m.requiredLits) == 0 {
		return true
	}
	for i := range m.requiredLits {
		if m.rulePossible(i, s, pos, ws) {
			return true
		}
	}
	return false
}

// rulePossible reports whether rule i could match at or after pos. A rule whose
// required literal is absent from the remainder cannot match in it. The mask is
// computed once per matcher per exec call and cached on the workspace: on R's
// tail rule set the identifier rule needs `<-`, absent from the input, and it
// was walked at all 610 candidates of every resume.
func (m *multiRegex) rulePossible(i int, s string, pos int, ws *jsWorkspace) bool {
	if i >= len(m.requiredLits) || m.requiredLits[i] == "" {
		return true
	}
	tail := ""
	if pos < len(s) {
		tail = s[pos:]
	}
	if ws == nil {
		return strings.Contains(tail, m.requiredLits[i])
	}
	if ws.ruleMask == nil {
		ws.ruleMask = make(map[*multiRegex]maskEntry)
	}
	entry, ok := ws.ruleMask[m]
	if !ok || entry.epoch != ws.maskEpoch {
		mask := make([]bool, len(m.requiredLits))
		for j, lit := range m.requiredLits {
			mask[j] = lit == "" || strings.Contains(tail, lit)
		}
		entry = maskEntry{epoch: ws.maskEpoch, mask: mask}
		ws.ruleMask[m] = entry
	}
	if i < len(entry.mask) {
		return entry.mask[i]
	}
	return true
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

// isBeginLineRule reports whether a prepared rule is only `^` (possibly in
// groups), which the m flag makes a begin-of-line assertion.
func isBeginLineRule(expr string) bool {
	re := parseExactPattern(expr, false)
	for re != nil && re.Op == syntax.OpCapture {
		re = re.Sub[0]
	}
	return re != nil && re.Op == syntax.OpBeginLine
}

// beginLineMatch is the empty match of the `^` rule at a line start, or nil
// when the rule set has none or pos is not a line start.
func (m *multiRegex) beginLineMatch(s string, pos int, jsLines bool) *match {
	if m.beginLineRule < 0 {
		return nil
	}
	if pos > 0 && !lineEndBefore(s, pos, jsLines) {
		return nil
	}
	group := m.ruleGroups[m.beginLineRule]
	data := m.matchIndexes[group]
	if data.keepGroups {
		return nil // the regexp reports the groups
	}
	return &match{
		index:     pos,
		typ:       data.typ,
		rule:      data.rule,
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
			if m.hasFirstBytes {
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
		// it once per candidate (D531).
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
// are tried in priority order, each by the exact walk (D523) or, failing
// that, ruled out by its prefilter (D522). The first rule that matches wins,
// as it does in the anchored alternation. decided is false at the first rule
// neither can settle, and the caller then runs the regexp.
//
// Both are bounded per rule (exactBudget steps, prefilterRun bytes), so a
// decision costs O(1) whatever the input's length.
func (m *multiRegex) decideAt(s string, pos int, jsLines bool, ws *jsWorkspace) (found *match, decided bool) {
	for i, prefilter := range m.prefilters {
		if !m.rulePossible(i, s, pos, ws) {
			continue
		}
		if exact := m.exacts[i]; exact != nil {
			end, result := matchExactAt(exact, s, pos, jsLines, ws)
			switch result {
			case exactMatched:
				data := m.matchIndexes[m.ruleGroups[i]]
				if data.keepGroups {
					return nil, false
				}
				return &match{
					index:     pos,
					lexeme:    s[pos:end],
					typ:       data.typ,
					rule:      data.rule,
					input:     s,
					position:  data.position,
					groupBase: m.ruleGroups[i],
					jsLines:   jsLines,
				}, true
			case exactNoMatch:
				continue
			}
		}
		if prefilter == nil || mayMatchAt(prefilter, s, pos, ws) {
			return nil, false
		}
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
func (r *resumableMultiRegex) exec(s string, lastIndex, regexIndex int, jsLines bool, ws *jsWorkspace) (*match, int) {
	matcher := r.getMatcher(regexIndex)
	result := matcher.exec(s, lastIndex, jsLines, ws)
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
