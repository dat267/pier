package markdown

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
func parseEmStrong(src string, i int, ch byte) (int, Inline, bool) {
	j := i
	for j < len(src) && src[j] == ch {
		j++
	}
	openLength := j - i

	prev, hasPrev := runeBefore(src, i)
	next, hasNext := runeAt(src, j)
	openLeft, openRight := delimiterFlanking(prev, hasPrev, next, hasNext)
	if !openLeft && !isCJKRune(prev) && !isCJKRune(next) {
		return 0, Inline{}, false
	}
	// An underscore opens only when it is left-flanking and either not also
	// right-flanking or preceded by punctuation, so it never opens intraword.
	if ch == '_' && openRight && !(hasPrev && isDelimiterPunctuation(prev)) {
		return 0, Inline{}, false
	}
	prevIsSameDelimiter := hasPrev && byte(prev) == ch

	clipped := src[j:]
	delimTotal := openLength
	midDelimTotal := 0
	for k := 0; k < len(clipped); {
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
		if ch == '_' && closeLeft && !closeRight && !(closeHasNext && isDelimiterPunctuation(closeNext)) && !cjkAdjacent {
			// An underscore closes only when it is right-flanking, and then only
			// when it is not also left-flanking or is followed by punctuation.
			// An intraword run is content, not a nested opener.
			k = runEnd
			continue
		}
		if ch == '_' && closeLeft && closeRight && !(closeHasNext && isDelimiterPunctuation(closeNext)) && !cjkAdjacent {
			// The same rule for a both-flanking intraword run: content.
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
