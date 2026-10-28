package highlight

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Mode is a highlight.js mode definition. Language files build these; the
// compiler turns them into compiledMode.
type Mode struct {
	ClassName      string
	Begin          string
	End            string
	BeginKeywords  string
	EndSameAsBegin bool
	EndsParent     bool
	EndsWithParent bool
	ExcludeBegin   bool
	ExcludeEnd     bool
	ReturnBegin    bool
	ReturnEnd      bool
	Skip           bool
	Illegal        string
	Match          string
	Keywords       Keywords
	Lexemes        string
	Contains       []*Mode
	Variants       []*Mode
	Starts         *Mode
	SubLanguage    []string
	Relevance      int
	HasRelevance   bool

	OnBegin func(m *match, resp *Response)
	OnEnd   func(m *match, resp *Response)

	// BeginNotFollowedBy and BeginFollowedBy lift a trailing `(?!...)` or
	// `(?=...)` lookahead (which RE2 cannot express) into a guard on the text
	// after the begin. The reference's `begin: 'x(?=y)'` becomes
	// `Begin: "x", BeginFollowedBy: "y"`.
	BeginNotFollowedBy string
	BeginFollowedBy    string

	// ManualBegin replaces the compiled begin regex with a Go matcher for rules
	// RE2 cannot express (backreferences). It returns the end offset, starting at
	// pos. ManualFirst is the byte such a match must start with.
	ManualBegin func(s string, pos int) (end int, ok bool)
	ManualFirst string

	beforeBegin func(m *match, resp *Response)

	cachedVariants []*Mode
}

// Keywords maps a className to a space-separated word list whose entries may
// carry a `|relevance` suffix. The special key "$pattern" is the keyword
// scanner pattern.
type Keywords map[string]string

// Language is a mode plus registration metadata.
type Language struct {
	Name              string
	Aliases           []string
	CaseInsensitive   bool
	DisableAutodetect bool
	ClassNameAliases  map[string]string
	Mode
}

// Self is the `contains: ['self']` sentinel.
var Self = &Mode{}

// Variant returns a copy of base with override applied, for mode.variants.
// When base itself has variants each one is copied with the same override, so
// inherit(STRINGS, {className: 'meta-string'}) keeps the string rules and only
// changes their class.
func Variant(base *Mode, override func(*Mode)) *Mode {
	c := cloneMode(base)
	override(c)
	if len(base.Variants) > 0 {
		variants := make([]*Mode, len(base.Variants))
		for i, v := range base.Variants {
			nv := cloneMode(v)
			override(nv)
			variants[i] = nv
		}
		c.Variants = variants
	}
	return c
}

// Zero returns a mode with an explicit relevance of 0.
func Zero(m Mode) *Mode {
	m.Relevance = 0
	m.HasRelevance = true
	return &m
}

type keywordData struct {
	className string
	relevance int
}

type compiledMode struct {
	className      string
	relevance      int
	skip           bool
	excludeBegin   bool
	excludeEnd     bool
	returnBegin    bool
	returnEnd      bool
	endSameAsBegin bool
	endsParent     bool
	endsWithParent bool

	keywords         map[string]keywordData
	keywordPatternRe *patternRegexp
	// keywordWord is set when the keyword pattern is the default `\w+`, which
	// can be scanned without the regexp engine (and its per-call allocation).
	keywordWord   bool
	beginRe       *patternRegexp
	beginSource   string
	endRe         *patternRegexp
	illegalRe     *patternRegexp
	illegalSource string
	terminatorEnd string
	contains      []*compiledMode
	starts        *compiledMode
	matcher       *resumableMultiRegex
	subLanguage   []string

	beforeBegin func(m *match, resp *Response)
	onBegin     func(m *match, resp *Response)
	onEnd       func(m *match, resp *Response)

	manualBegin func(s string, pos int) (end int, ok bool)
	manualFirst string
	manual      bool

	// keepGroups is set when a callback may read the match's capture groups, so
	// the inner groups must survive into the compiled matcher. Most rules only
	// need the wrapper group that identifies the rule.
	keepGroups bool
}

// Response is highlight.js's Response: callbacks can ignore the match.
type Response struct {
	data           map[string]string
	isMatchIgnored bool
}

// IgnoreMatch tells the engine not to apply the current rule.
func (r *Response) IgnoreMatch() { r.isMatchIgnored = true }

type compiler struct {
	language *Language
	compiled map[*Mode]*compiledMode
}

func (c *compiler) langRe(src string) (*patternRegexp, error) {
	flags := "m"
	if c.language.CaseInsensitive {
		flags += "i"
	}
	return compilePattern(src, flags)
}

func (c *compiler) compileMode(mode *Mode, parent *compiledMode) (*compiledMode, error) {
	// The reference sets isCompiled before compiling `contains`, so a language
	// may reference itself (JSON's value container holds its own object and
	// array modes). Memoising the in-progress compile keeps that graph finite.
	if existing, ok := c.compiled[mode]; ok {
		return existing, nil
	}
	if mode.BeginFollowedBy != "" || mode.BeginNotFollowedBy != "" {
		followedRe, followedErr := c.langRe(mode.BeginFollowedBy)
		notFollowedRe, notFollowedErr := c.langRe(mode.BeginNotFollowedBy)
		if followedErr != nil {
			return nil, followedErr
		}
		if notFollowedErr != nil {
			return nil, notFollowedErr
		}
		previous := mode.beforeBegin
		mode.beforeBegin = func(m *match, resp *Response) {
			if previous != nil {
				previous(m, resp)
				if resp.isMatchIgnored {
					return
				}
			}
			rest := m.input[m.index+len(m.lexeme):]
			if mode.BeginFollowedBy != "" && !startsWith(followedRe, rest) {
				resp.IgnoreMatch()
				return
			}
			if mode.BeginNotFollowedBy != "" && startsWith(notFollowedRe, rest) {
				resp.IgnoreMatch()
			}
		}
	}
	cmode := &compiledMode{
		className:      mode.ClassName,
		skip:           mode.Skip,
		excludeBegin:   mode.ExcludeBegin,
		excludeEnd:     mode.ExcludeEnd,
		returnBegin:    mode.ReturnBegin,
		returnEnd:      mode.ReturnEnd,
		endSameAsBegin: mode.EndSameAsBegin,
		endsParent:     mode.EndsParent,
		endsWithParent: mode.EndsWithParent,
		beforeBegin:    mode.beforeBegin,
		onBegin:        mode.OnBegin,
		onEnd:          mode.OnEnd,
		subLanguage:    mode.SubLanguage,
		keepGroups:     mode.OnBegin != nil || mode.OnEnd != nil,
	}
	c.compiled[mode] = cmode

	if mode.Match != "" {
		if mode.Begin != "" || mode.End != "" {
			return nil, fmt.Errorf("highlight: begin & end are not supported with match")
		}
		mode.Begin = mode.Match
	}

	// beginKeywords syntactic sugar.
	if parent != nil && mode.BeginKeywords != "" {
		words := strings.Split(mode.BeginKeywords, " ")
		// JavaScript's `(?=\b|\s)` lookahead backtracks to a longer alternative
		// when the boundary after a shorter prefix fails (clojure's `def` vs
		// `defn`, gams' `set` vs `sets`); RE2's leftmost-first alternation does
		// not, so try the longest word first.
		sort.SliceStable(words, func(i, j int) bool { return len(words[i]) > len(words[j]) })
		mode.Begin = `\b(` + strings.Join(words, "|") + `)`
		preceding := mode.beforeBegin
		mode.beforeBegin = func(m *match, resp *Response) {
			if preceding != nil {
				preceding(m, resp)
				if resp.isMatchIgnored {
					return
				}
			}
			if m.index > 0 && m.input[m.index-1] == '.' {
				resp.IgnoreMatch()
				return
			}
			// The reference's `(?!\.)(?=\b|\s)` suffix: after the match the next
			// character must be absent or a non-word character other than `.`.
			next := m.index + len(m.lexeme)
			if next < len(m.input) {
				b := m.input[next]
				if b == '.' || isWordByte(b) {
					resp.IgnoreMatch()
				}
			}
		}
		cmode.beforeBegin = mode.beforeBegin
		if mode.Keywords == nil {
			mode.Keywords = Keywords{"keyword": mode.BeginKeywords}
		}
		if !mode.HasRelevance {
			mode.Relevance = 0
			mode.HasRelevance = true
		}
		mode.BeginKeywords = ""
	}

	if mode.HasRelevance {
		cmode.relevance = mode.Relevance
	} else {
		cmode.relevance = 1
	}

	keywordPattern := ""
	if mode.Keywords != nil {
		keywordPattern = mode.Keywords["$pattern"]
		delete(mode.Keywords, "$pattern")
		cmode.keywords = compileKeywords(mode.Keywords, c.language.CaseInsensitive)
	}
	if mode.Lexemes != "" && keywordPattern != "" {
		return nil, fmt.Errorf("highlight: prefer keywords.$pattern to mode.lexemes")
	}
	if keywordPattern == "" {
		keywordPattern = mode.Lexemes
	}
	if keywordPattern == "" {
		keywordPattern = `\w+`
	}
	re, err := c.langRe(keywordPattern)
	if err != nil {
		return nil, err
	}
	cmode.keywordPatternRe = re
	cmode.keywordWord = keywordPattern == `\w+`

	if mode.ManualBegin != nil {
		cmode.manual = true
		cmode.manualBegin = mode.ManualBegin
		cmode.manualFirst = mode.ManualFirst
	}
	if parent != nil {
		if mode.Begin == "" && !cmode.manual {
			mode.Begin = `\B|\b`
		}
		if !cmode.manual {
			beginRe, err := c.langRe(mode.Begin)
			if err != nil {
				return nil, err
			}
			cmode.beginRe = beginRe
			cmode.beginSource = mode.Begin
		}
		if mode.EndSameAsBegin {
			mode.End = mode.Begin
		}
		if mode.End == "" && !mode.EndsWithParent {
			mode.End = `\B|\b`
		}
		if mode.End != "" {
			endRe, err := c.langRe(mode.End)
			if err != nil {
				return nil, err
			}
			cmode.endRe = endRe
		}
		cmode.terminatorEnd = mode.End
		if mode.EndsWithParent && parent.terminatorEnd != "" {
			if mode.End != "" {
				cmode.terminatorEnd += "|"
			}
			cmode.terminatorEnd += parent.terminatorEnd
		}
	}
	if mode.Illegal != "" {
		illegalRe, err := c.langRe(mode.Illegal)
		if err != nil {
			return nil, err
		}
		cmode.illegalRe = illegalRe
		cmode.illegalSource = mode.Illegal
	}

	contains := mode.Contains
	if contains == nil {
		contains = nil
	}
	expanded := make([]*Mode, 0, len(contains))
	for _, child := range contains {
		if child == Self {
			child = mode
		}
		expanded = append(expanded, expandOrCloneMode(child)...)
	}
	for _, child := range expanded {
		compiledChild, err := c.compileMode(child, cmode)
		if err != nil {
			return nil, err
		}
		cmode.contains = append(cmode.contains, compiledChild)
	}
	if mode.Starts != nil {
		compiledStarts, err := c.compileMode(mode.Starts, parent)
		if err != nil {
			return nil, err
		}
		cmode.starts = compiledStarts
	}
	cmode.matcher = c.buildModeRegex(cmode)
	return cmode, nil
}

func (c *compiler) buildModeRegex(mode *compiledMode) *resumableMultiRegex {
	mm := &resumableMultiRegex{caseIgnore: c.language.CaseInsensitive}
	for _, term := range mode.contains {
		if term.manual {
			mm.addManual(term.manualBegin, term.manualFirst, matchData{rule: term, typ: ruleBegin})
		} else {
			mm.addRule(term.beginSource, term.keepGroups, matchData{rule: term, typ: ruleBegin})
		}
	}
	if mode.terminatorEnd != "" {
		mm.addRule(mode.terminatorEnd, true, matchData{typ: ruleEnd})
	}
	if mode.illegalSource != "" {
		mm.addRule(mode.illegalSource, false, matchData{typ: ruleIllegal})
	}
	return mm
}

func compileKeywords(raw Keywords, caseInsensitive bool) map[string]keywordData {
	out := map[string]keywordData{}
	var compileList func(className, words string)
	compileList = func(className, words string) {
		for keyword := range strings.SplitSeq(words, " ") {
			if keyword == "" {
				continue
			}
			key := keyword
			if caseInsensitive {
				key = strings.ToLower(key)
			}
			pair := strings.SplitN(keyword, "|", 2)
			word := pair[0]
			if caseInsensitive {
				word = strings.ToLower(word)
			}
			out[word] = keywordData{className: className, relevance: scoreForKeyword(strings.SplitN(keyword, "|", 2)...)}
		}
	}
	for _, className := range orderedKeywordClasses(raw) {
		compileList(className, raw[className])
	}
	return out
}

// keywordClassOrder is the order classes are applied when a word appears in
// more than one. The reference iterates the keyword object in insertion order
// and the last class wins; a Go map has no order, so list the common classes
// first (with built_in last, which is what TypeScript's KEYWORDS relies on for
// `void`) and sort the rest for determinism.
var keywordClassOrder = []string{"keyword", "literal", "built_in", "type", "title", "symbol", "meta"}

func orderedKeywordClasses(raw Keywords) []string {
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, name := range keywordClassOrder {
		if _, ok := raw[name]; ok && !seen[name] {
			out = append(out, name)
			seen[name] = true
		}
	}
	rest := make([]string, 0, len(raw))
	for name := range raw {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

func scoreForKeyword(parts ...string) int {
	keyword := parts[0]
	if len(parts) > 1 && parts[1] != "" {
		n, err := strconv.Atoi(parts[1])
		if err == nil {
			return n
		}
	}
	if commonKeyword(keyword) {
		return 0
	}
	return 1
}

var commonKeywords = map[string]bool{
	"of": true, "and": true, "for": true, "in": true, "not": true,
	"or": true, "if": true, "then": true, "parent": true, "list": true, "value": true,
}

func commonKeyword(keyword string) bool { return commonKeywords[strings.ToLower(keyword)] }

func dependencyOnParent(mode *Mode) bool {
	if mode == nil {
		return false
	}
	return mode.EndsWithParent || dependencyOnParent(mode.Starts)
}

func expandOrCloneMode(mode *Mode) []*Mode {
	if len(mode.Variants) > 0 {
		if mode.cachedVariants == nil {
			mode.cachedVariants = mode.Variants
		}
		return mode.cachedVariants
	}
	if dependencyOnParent(mode) {
		clone := cloneMode(mode)
		if mode.Starts != nil {
			clone.Starts = cloneMode(mode.Starts)
		}
		return []*Mode{clone}
	}
	return []*Mode{mode}
}

// cloneTree deep-clones a mode tree for one language's compilation.
func cloneTree(m *Mode, seen map[*Mode]*Mode) *Mode {
	if m == nil || m == Self {
		return m
	}
	if clone, ok := seen[m]; ok {
		return clone
	}
	clone := cloneMode(m)
	seen[m] = clone
	clone.Contains = nil
	for _, child := range m.Contains {
		clone.Contains = append(clone.Contains, cloneTree(child, seen))
	}
	if len(m.Variants) > 0 {
		clone.Variants = clone.Variants[:0]
		for _, v := range m.Variants {
			clone.Variants = append(clone.Variants, cloneTree(v, seen))
		}
	}
	if m.Starts != nil {
		clone.Starts = cloneTree(m.Starts, seen)
	}
	return clone
}

func cloneMode(m *Mode) *Mode {
	c := *m
	if m.Contains != nil {
		c.Contains = append([]*Mode(nil), m.Contains...)
	}
	if m.Variants != nil {
		c.Variants = append([]*Mode(nil), m.Variants...)
	}
	return &c
}

// stripCapturingGroups rewrites inner capturing groups to non-capturing ones.
// The wrapper group added when joining rules still identifies the rule, so only
// modes whose callbacks read groups keep their inner captures.
func stripCapturingGroups(src string) string {
	if !strings.Contains(src, "(") {
		return src
	}
	var b strings.Builder
	b.Grow(len(src))
	inClass := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if c == '\\' && i+1 < len(src) {
			b.WriteByte(c)
			b.WriteByte(src[i+1])
			i++
			continue
		}
		if inClass {
			if c == ']' {
				inClass = false
			}
			b.WriteByte(c)
			continue
		}
		if c == '[' {
			inClass = true
			b.WriteByte(c)
			continue
		}
		if c == '(' {
			if i+1 < len(src) && src[i+1] == '?' {
				// (?P<name>...) is capturing; drop the name and make it
				// non-capturing.
				if i+2 < len(src) && src[i+2] == 'P' {
					if end := strings.IndexByte(src[i:], '>'); end >= 0 {
						b.WriteString("(?:")
						i += end
						continue
					}
				}
				b.WriteByte(c)
				continue
			}
			b.WriteString("(?:")
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// countCapturingGroups counts capturing groups syntactically, before the
// joined alternation is compiled.
func countCapturingGroups(src string) int {
	count := 0
	inClass := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if c == '\\' {
			i++
			continue
		}
		if inClass {
			if c == ']' {
				inClass = false
			}
			continue
		}
		if c == '[' {
			inClass = true
			continue
		}
		if c == '(' {
			if i+1 < len(src) && src[i+1] == '?' {
				if i+2 < len(src) && src[i+2] == 'P' {
					count++
				}
				continue
			}
			count++
		}
	}
	return count
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
