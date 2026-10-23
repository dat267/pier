package markdown

import (
	"regexp"
	"unicode"
	"unicode/utf8"
)

// The first right-delimiter alternative masks a mixed-marker span at the start
// of the clipped input. Consume the final pair only to implement its lookahead,
// then leave that pair for the following delimiter scan.
var emphasisSkipAsterisk = regexp.MustCompile(`^[^_*]*?__[^_*]*?\*[^_*]*?__`)
var emphasisSkipUnderscore = regexp.MustCompile(`^[^_*]*?\*\*[^_*]*?_[^_*]*?\*\*`)

func emphasisScanStart(src string, ch byte) int {
	pattern := emphasisSkipAsterisk
	if ch == '_' {
		pattern = emphasisSkipUnderscore
	}
	if match := pattern.FindStringIndex(src); match != nil {
		return match[1] - 2
	}
	return 0
}

// Emphasis runs, ported from Marked's emStrong tokenizer as upstream v1.4.0
// configures it (marked 18 + marked-cjk-friendly, b2d0da6fe, #10154).
//
// Marked scans a left delimiter, then walks the right delimiters that follow,
// accumulating their length until the opener is spent. Runs that can only open
// add to the total, a "both-flanking" run may be skipped when the rule of three
// applies, and the accepted run is trimmed to exactly what the opener needs.
// CJK neighbours are treated as punctuation, which is what makes emphasis such
// as `**这是测试。**后面` pair.

// parseEmStrong parses *…* / **…** / ***…*** / _…_ starting at i.
func parseEmStrong(src string, i int, ch byte, prev rune, hasPrev bool) (int, Inline, bool) {
	j := i
	for j < len(src) && src[j] == ch {
		j++
	}
	openLength := j - i

	next, hasNext := runeAt(src, j)
	if !hasNext || isDelimiterWhitespace(next) {
		return 0, Inline{}, false
	}
	// Marked sees a UTF-16 code unit here; only the CJK extension recovers
	// supplementary predecessors for its punctuation override.
	if prev > 0xffff {
		prev = 0xdc00 + ((prev - 0x10000) & 0x3ff)
	}
	// The left-delimiter regex distinguishes punctuation from word content
	// before applying the CJK override. GFM treats tilde as word content for *.
	nextPunct := unicode.IsPunct(next) || unicode.IsSymbol(next)
	if ch == '*' && next == '~' {
		nextPunct = false
	}
	if ch == '_' && !nextPunct && hasPrev && (unicode.IsLetter(prev) || unicode.IsNumber(prev)) {
		return 0, Inline{}, false
	}
	prevPunct := prev != '*' && prev != '_' && (isDelimiterWhitespace(prev) || isDelimiterPunctuation(prev))
	prevCJK := false
	if hasPrev && !prevPunct {
		before, _ := runeBefore(src, i)
		if before >= 0xfe00 && before <= 0xfe0e {
			before, _ = runeBefore(src, i-utf8.RuneLen(before))
		}
		prevCJK = isCJKRune(before) || before >= 0xe0100 && before <= 0xe01ef
	}
	if nextPunct && hasPrev && !prevPunct && !prevCJK && !isCJKRune(next) {
		return 0, Inline{}, false
	}
	prevIsSameDelimiter := hasPrev && prev == rune(ch)

	clipped := src[j:]
	delimTotal := openLength
	midDelimTotal := 0
	for k := emphasisScanStart(clipped, ch); k < len(clipped); {
		if clipped[k] != ch {
			k++
			continue
		}
		runEnd := k
		for runEnd < len(clipped) && clipped[runEnd] == ch {
			runEnd++
		}
		closeLength := runEnd - k
		runStart := j + k
		closePrev, closeHasPrev := runeBefore(src, runStart)
		closeNext, closeHasNext := runeAt(src, j+runEnd)
		closeLeft, closeRight := delimiterFlanking(closePrev, closeHasPrev, closeNext, closeHasNext)
		cjkAdjacent := isCJKRune(closePrev) || isCJKRune(closeNext)

		if !closeLeft && !closeRight && !cjkAdjacent {
			// Not a delimiter at all: content.
			k = runEnd
			continue
		}
		if ch == '_' && closeLeft && closeRight && !(closeHasNext && isDelimiterPunctuation(closeNext)) && !cjkAdjacent {
			// A both-flanking intraword underscore run is content.
			k = runEnd
			continue
		}
		if closeLeft && !closeRight && !cjkAdjacent {
			// Can only open: it belongs to a nested run.
			delimTotal += closeLength
			k = runEnd
			continue
		}
		if closeLeft && closeRight || cjkAdjacent {
			// Rule of three: a run whose length makes the pair a multiple of
			// three cannot close an opener that is itself not a multiple.
			if openLength%3 != 0 && (openLength+closeLength)%3 == 0 {
				midDelimTotal += closeLength
				k = runEnd
				continue
			}
			if prevIsSameDelimiter && closeLeft && closeRight {
				break
			}
		}
		delimTotal -= closeLength
		if delimTotal > 0 {
			k = runEnd
			continue
		}
		consumed := closeLength
		if adjusted := closeLength + delimTotal + midDelimTotal; adjusted < consumed {
			consumed = adjusted
		}
		if consumed <= 0 {
			k = runEnd
			continue
		}
		end := runStart + consumed
		raw := src[i:end]
		if openLength < consumed {
			if openLength%2 != 0 {
				return end, Inline{Kind: InlineEmphasis, Text: raw[1 : len(raw)-1], Children: ParseInline(raw[1 : len(raw)-1])}, true
			}
			return end, Inline{Kind: InlineStrong, Text: raw[2 : len(raw)-2], Children: ParseInline(raw[2 : len(raw)-2])}, true
		}
		if consumed%2 != 0 {
			return end, Inline{Kind: InlineEmphasis, Text: raw[1 : len(raw)-1], Children: ParseInline(raw[1 : len(raw)-1])}, true
		}
		return end, Inline{Kind: InlineStrong, Text: raw[2 : len(raw)-2], Children: ParseInline(raw[2 : len(raw)-2])}, true
	}
	return 0, Inline{}, false
}
