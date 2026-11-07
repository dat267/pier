package tui

import (
	"sort"
	"unicode/utf8"
)

// Word segmentation: upstream segments with Intl.Segmenter (granularity
// "word"), which is ICU 78's rule-based word break iterator (UAX #29 as
// tailored by icu4c/source/data/brkitr/rules/word.txt) on Unicode 17. This
// file implements those rules over the table scripts/gen_wordbreak.py
// generates (wordbreak_table.go). ICU's dictionary break engines then split
// runs of Han, kana and Complex_Context (Thai, Lao, Khmer, Myanmar) text into
// words; pier keeps such a run as the one segment the rules produce (D399).

type wbClass uint8

// wbProps is a code point's class (low five bits) and flags.
type wbProps uint16

const (
	wbClassMask      wbProps = 0x1f
	wbExtPict        wbProps = 1 << 5
	wbHiragana       wbProps = 1 << 6
	wbIdeographic    wbProps = 1 << 7
	wbKanaKanji      wbProps = 1 << 8
	wbInvalidUTF8    wbProps = 1 << 15 // a byte that does not start a valid sequence
	wbStatusNone             = 0
	wbStatusNumber           = 100
	wbStatusLetter           = 200
	wbStatusKanaIdeo         = 400
)

type wbRange struct {
	lo, hi rune
	props  wbProps
}

var wbASCII = func() (table [utf8.RuneSelf]wbProps) {
	for _, r := range wbRanges {
		if r.lo >= utf8.RuneSelf {
			break
		}
		for c := r.lo; c <= r.hi && c < utf8.RuneSelf; c++ {
			table[c] = r.props
		}
	}
	return table
}()

func wbLookup(r rune) wbProps {
	if r < utf8.RuneSelf {
		return wbASCII[r]
	}
	i := sort.Search(len(wbRanges), func(i int) bool { return wbRanges[i].hi >= r })
	if i < len(wbRanges) && wbRanges[i].lo <= r {
		return wbRanges[i].props
	}
	return 0
}

func (p wbProps) class() wbClass { return wbClass(p & wbClassMask) }

// exFm is word.txt's $ExFm = [$Extend $Format $ZWJ] (WB4).
func (p wbProps) exFm() bool {
	switch p.class() {
	case wbExtend, wbFormat, wbZWJ:
		return p&wbInvalidUTF8 == 0
	}
	return false
}

// ahLetter is ($ALetterPlus | $Hebrew_Letter).
func (p wbProps) ahLetter() bool {
	c := p.class()
	return c == wbALetter || c == wbHebrewLetter
}

func (p wbProps) midLetterQ() bool {
	c := p.class()
	return c == wbMidLetter || c == wbMidNumLet || c == wbSingleQuote
}

func (p wbProps) midNumQ() bool {
	c := p.class()
	return c == wbMidNum || c == wbMidNumLet || c == wbSingleQuote
}

func (p wbProps) lineBreak() bool {
	c := p.class()
	return c == wbCR || c == wbLF || c == wbNewline
}

// singleStatus is the status of word.txt's one-character rules (status
// 100-400 for a word's final base character, none otherwise).
func (p wbProps) singleStatus() int {
	if p&wbInvalidUTF8 != 0 {
		return wbStatusNone
	}
	switch p.class() {
	case wbNumeric:
		return wbStatusNumber
	case wbALetter, wbHangul, wbHebrewLetter:
		return wbStatusLetter
	case wbKatakana:
		return wbStatusKanaIdeo
	}
	if p&(wbHiragana|wbIdeographic) != 0 {
		return wbStatusKanaIdeo
	}
	return wbStatusNone
}

// wordBreaker walks text one code point at a time and reports, for each code
// point after the first, whether a word boundary precedes it.
type wordBreaker struct {
	text string
	// prevRaw is the code point just before the current one; last and
	// beforeLast are the last two characters WB4 does not absorb.
	prevRaw, last, beforeLast wbProps
	// bases counts the current segment's characters WB4 did not absorb, and
	// riCount its regional indicators.
	bases, riCount int
	status         int
	// lastAdjacent is whether last is the code point just before the current
	// one: word.txt's CJK chains and its Hangul status rule take no $ExFm*.
	lastAdjacent bool
}

func decodeWordBreak(text string, at int) (wbProps, int) {
	r, size := utf8.DecodeRuneInString(text[at:])
	if r == utf8.RuneError && size <= 1 {
		return wbInvalidUTF8, 1
	}
	return wbLookup(r), size
}

// nextBase returns the first code point at or after at that WB4 does not
// absorb, or ok=false at the end of the text.
func (b *wordBreaker) nextBase(at int) (wbProps, bool) {
	for at < len(b.text) {
		p, size := decodeWordBreak(b.text, at)
		if !p.exFm() {
			return p, true
		}
		at += size
	}
	return 0, false
}

// start begins a segment at a code point.
func (b *wordBreaker) start(p wbProps) {
	b.prevRaw, b.beforeLast, b.last = p, b.last, p
	b.bases, b.riCount = 1, 0
	b.lastAdjacent = true
	if p.class() == wbRegionalIndicator {
		b.riCount = 1
	}
	b.status = p.singleStatus()
}

// joins reports whether cur (at byte offset at, after its size bytes the rest
// of the text follows) continues the segment, and updates the state.
func (b *wordBreaker) joins(cur wbProps, at, size int) bool {
	prev := b.prevRaw
	invalid := (cur|prev)&wbInvalidUTF8 != 0
	switch {
	case invalid:
		// Pier splits undecodable bytes into segments of their own.
	case prev.class() == wbCR && cur.class() == wbLF: // WB3
		b.prevRaw = cur
		return true
	case prev.lineBreak() || cur.lineBreak(): // WB3a, WB3b
	case prev.class() == wbZWJ && cur&wbExtPict != 0: // WB3c
		b.advance(cur, wbStatusNone)
		return true
	case prev.class() == wbWSegSpace && cur.class() == wbWSegSpace: // WB3d
		b.advance(cur, wbStatusNone)
		return true
	case cur.exFm(): // WB4
		b.attach(cur)
		return true
	default:
		if status, ok := b.rule(cur, at+size); ok {
			b.advance(cur, status)
			return true
		}
	}
	b.start(cur)
	return false
}

// rule applies WB5-WB16 and word.txt's CJK chains between the last base
// character and cur. It returns the joining rule's status.
func (b *wordBreaker) rule(cur wbProps, after int) (int, bool) {
	last, beforeLast := b.last, b.beforeLast
	lc, cc := last.class(), cur.class()
	switch {
	case last.ahLetter() && cur.ahLetter(): // WB5
		return wbStatusNone, true
	case last.ahLetter() && cur.midLetterQ(): // WB6
		if next, ok := b.nextBase(after); ok && next.ahLetter() {
			return wbStatusNone, true
		}
	case beforeLast.ahLetter() && last.midLetterQ() && cur.ahLetter() && b.inSegment(2): // WB7
		return wbStatusLetter, true
	}
	switch {
	case lc == wbHebrewLetter && cc == wbSingleQuote: // WB7a
		return wbStatusLetter, true
	case lc == wbHebrewLetter && cc == wbDoubleQuote: // WB7b
		if next, ok := b.nextBase(after); ok && next.class() == wbHebrewLetter {
			return wbStatusNone, true
		}
	case beforeLast.class() == wbHebrewLetter && lc == wbDoubleQuote && cc == wbHebrewLetter && b.inSegment(2): // WB7c
		return wbStatusNone, true
	case lc == wbNumeric && cc == wbNumeric, // WB8
		last.ahLetter() && cc == wbNumeric, // WB9
		lc == wbNumeric && cur.ahLetter():  // WB10
		return wbStatusNone, true
	case beforeLast.class() == wbNumeric && last.midNumQ() && cc == wbNumeric && b.inSegment(2): // WB11
		return wbStatusNone, true
	case lc == wbNumeric && cur.midNumQ(): // WB12
		if next, ok := b.nextBase(after); ok && next.class() == wbNumeric {
			return wbStatusNone, true
		}
	case lc == wbKatakana && cc == wbKatakana: // WB13
		return wbStatusKanaIdeo, true
	case cc == wbExtendNumLet && (last.ahLetter() || lc == wbNumeric || lc == wbKatakana || lc == wbExtendNumLet): // WB13a
		switch lc {
		case wbNumeric:
			return wbStatusNumber, true
		case wbKatakana:
			return wbStatusKanaIdeo, true
		}
		return wbStatusLetter, true
	case lc == wbExtendNumLet && (cur.ahLetter() || cc == wbNumeric || cc == wbKatakana): // WB13b
		return wbStatusNone, true
	case lc == wbRegionalIndicator && cc == wbRegionalIndicator && b.riCount == 1: // WB15, WB16
		return wbStatusNone, true
	case b.lastAdjacent && lc == wbHangul && cc == wbHangul: // $HangulSyllable $HangulSyllable
		return wbStatusLetter, true
	case b.lastAdjacent && last&wbKanaKanji != 0 && cur&wbKanaKanji != 0: // $KanaKanji $KanaKanji
		return wbStatusKanaIdeo, true
	}
	return 0, false
}

// inSegment reports whether the current segment holds at least n base
// characters, so beforeLast belongs to it.
func (b *wordBreaker) inSegment(n int) bool { return b.bases >= n }

// attach absorbs an Extend, Format or ZWJ code point into the segment (WB4).
// ICU takes the largest status among the rules that end here: the rule for
// the last base character with its trailing $ExFm* (which $HangulSyllable
// and the joining rules lack, so they end with no status), and the rule for
// an extending character that is itself Ideographic or Hiragana.
func (b *wordBreaker) attach(cur wbProps) {
	own := wbStatusNone
	if cur&(wbHiragana|wbIdeographic) != 0 {
		own = wbStatusKanaIdeo
	}
	if b.lastAdjacent {
		base := wbStatusNone
		if b.last.class() != wbHangul {
			base = b.last.singleStatus()
		}
		b.status = max(base, own)
	} else {
		b.status = max(b.status, own)
	}
	b.prevRaw = cur
	b.lastAdjacent = false
}

func (b *wordBreaker) advance(cur wbProps, ruleStatus int) {
	b.prevRaw, b.beforeLast, b.last = cur, b.last, cur
	b.lastAdjacent = true
	b.bases++
	if cur.class() == wbRegionalIndicator {
		b.riCount++
	}
	b.status = max(cur.singleStatus(), ruleStatus)
}

// segmentWords splits text into word segments. Index counts each undecodable
// byte as the three bytes of its U+FFFD replacement, which is also the
// segment text it becomes.
func segmentWords(text string) []WordSegment {
	if text == "" {
		return nil
	}
	// A short line needs one small allocation; a long one grows by doubling,
	// which keeps a single long word (one segment) from reserving space for
	// many (TestEditorWordNavigationDoesNotCopyRuneTables).
	segments := make([]WordSegment, 0, min(len(text)/4+1, 32))
	b := wordBreaker{text: text}
	first, size := decodeWordBreak(text, 0)
	b.start(first)
	segStart, segIndex, index := 0, 0, wbUnitLength(first, size)
	for at := size; at < len(text); {
		var cur wbProps
		curSize := 1
		if c := text[at]; c < utf8.RuneSelf {
			cur = wbASCII[c]
			// WB5 and WB8-WB10: a letter or digit after a letter or digit
			// always continues the word, so take an ASCII run of them at once.
			if wbAlnum(cur) && wbAlnum(b.last) {
				end := at + 1
				for end < len(text) && text[end] < utf8.RuneSelf && wbAlnum(wbASCII[text[end]]) {
					end++
				}
				if end-at > 1 {
					b.beforeLast = wbASCII[text[end-2]]
				} else {
					b.beforeLast = b.last
				}
				b.last = wbASCII[text[end-1]]
				b.prevRaw, b.lastAdjacent = b.last, true
				b.bases += end - at
				b.status = b.last.singleStatus()
				index += end - at
				at = end
				continue
			}
			// No rule joins ASCII punctuation of class Other ("/", "(", "!",
			// "-", ...) or a space that does not follow one (WB3d) to what came
			// before, and ASCII holds no Extended_Pictographic (WB3c), so they
			// always start a segment.
			if c := cur.class(); c == wbOther || c == wbWSegSpace && b.prevRaw.class() != wbWSegSpace {
				segments = appendWordSegment(segments, text, segStart, at, segIndex, b.status, b.prevRaw&wbInvalidUTF8 != 0)
				segStart, segIndex = at, index
				b.start(cur)
				index++
				at++
				continue
			}
		} else {
			cur, curSize = decodeWordBreak(text, at)
		}
		status, invalid := b.status, b.prevRaw&wbInvalidUTF8 != 0
		if !b.joins(cur, at, curSize) {
			segments = appendWordSegment(segments, text, segStart, at, segIndex, status, invalid)
			segStart, segIndex = at, index
		}
		index += wbUnitLength(cur, curSize)
		at += curSize
	}
	return appendWordSegment(segments, text, segStart, len(text), segIndex, b.status, b.prevRaw&wbInvalidUTF8 != 0)
}

// wbAlnum is a letter (ALetter or Hebrew_Letter) or digit (Numeric).
func wbAlnum(p wbProps) bool {
	switch p.class() {
	case wbALetter, wbHebrewLetter, wbNumeric:
		return p&wbInvalidUTF8 == 0
	}
	return false
}

func appendWordSegment(segments []WordSegment, text string, start, end, index, status int, invalid bool) []WordSegment {
	segment := text[start:end]
	if invalid {
		segment = string(utf8.RuneError)
	}
	return append(segments, WordSegment{Segment: segment, Index: index, IsWordLike: status >= wbStatusNumber})
}

func wbUnitLength(p wbProps, size int) int {
	if p&wbInvalidUTF8 != 0 {
		return utf8.RuneLen(utf8.RuneError)
	}
	return size
}
