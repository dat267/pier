package highlight

import (
	"regexp/syntax"
	"strings"
	"sync"
	"unicode/utf8"
)

// ignoredIllegalBatch coalesces ignored character-class illegals, which only
// append text in highlight.js's processLexeme. Begin/end rules still win ties.
// Multi-character and zero-width illegals stay on the ordinary scanner: they
// can consume across another rule or have special cursor semantics.
// D348 records the M140 workload, reference measurements and parity tests.
type ignoredIllegalBatch struct {
	ascii   [utf8.RuneSelf]bool
	ranges  []rune
	once    sync.Once
	matcher *patternRegexp
}

func newIgnoredIllegalBatch(mode *compiledMode) *ignoredIllegalBatch {
	if mode.illegalRe == nil {
		return nil
	}
	for _, rule := range mode.matcher.rules {
		if rule.manual != nil {
			return nil
		}
	}
	expr, err := syntax.Parse(mode.illegalRe.String(), syntax.Perl)
	if err != nil || expr.Op != syntax.OpCharClass {
		return nil
	}
	batch := &ignoredIllegalBatch{ranges: expr.Rune}
	for i := range batch.ascii {
		batch.ascii[i] = batch.matches(rune(i))
	}
	return batch
}

func (b *ignoredIllegalBatch) matches(r rune) bool {
	for i := 0; i < len(b.ranges); i += 2 {
		if r < b.ranges[i] {
			return false
		}
		if r <= b.ranges[i+1] {
			return true
		}
	}
	return false
}

func (b *ignoredIllegalBatch) getMatcher(mode *compiledMode) *patternRegexp {
	b.once.Do(func() {
		var rules []string
		for _, rule := range mode.matcher.rules {
			if rule.data.typ != ruleIllegal {
				rules = append(rules, stripCapturingGroups(encodeCodeUnitsString(rule.src)))
			}
		}
		if len(rules) == 0 {
			return
		}
		flags := "m"
		if mode.matcher.caseIgnore {
			flags += "i"
		}
		// Consume the real preceding rune before searching a meaningful token.
		// This keeps ^/\b in context without a verify/retry at every digit, and
		// needs neither captures nor the winning rule's identity.
		b.matcher, _ = compileEncodedPattern(`(?s:.)(?:`+strings.Join(rules, "|")+`)`, flags)
	})
	return b.matcher
}

// extend covers the inert text before the next begin/end token with one illegal
// match. Count individual illegals to preserve the loop's hang guard.
func (b *ignoredIllegalBatch) extend(h *highlighter, m *match) {
	scanner := b.getMatcher(h.top.mode)
	if scanner == nil {
		return
	}
	end := len(m.input)
	start := m.index + len(m.lexeme)
	_, width := utf8.DecodeLastRuneInString(m.input[:start])
	base := start - width
	if next := scanner.findIndexWS(m.input[base:], h.jsLines, &h.regexWS); next != nil {
		_, prefixWidth := utf8.DecodeRuneInString(m.input[base+next[0]:])
		end = base + next[0] + prefixWidth
	}
	count := 0
	remaining := max(1, maxHighlightIterations+2-h.iterations)
	for pos := m.index; pos < end; {
		r, width := rune(m.input[pos]), 1
		matched := false
		if r < utf8.RuneSelf {
			matched = b.ascii[r]
		} else {
			r, width = utf8.DecodeRuneInString(m.input[pos:])
			matched = b.matches(r)
		}
		pos += width
		if matched {
			count++
			if count == remaining {
				end = pos
				break
			}
		}
	}
	h.iterations += count - 1 // The loop already counted the first illegal.
	m.lexeme = m.input[m.index:end]
}
