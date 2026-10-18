// Package tui is the Go port of @earendil-works/pi-tui: the terminal UI
// library with differential rendering behind the interactive coding agent.
//
// This file ports src/utils.ts: terminal-cell width calculation over grapheme
// clusters, and terminal-sequence handling (CSI/OSC/APC).
package tui

import (
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/width"
)

// Grapheme segmentation: Go has no Intl.Segmenter, so the port implements the
// practical UAX #29 subset the width math needs (base + combining marks, ZWJ
// sequences, variation selectors, skin tones, regional indicators).

// segmentGraphemes splits text into grapheme clusters.
func segmentGraphemes(text string) []string {
	if text == "" {
		return nil
	}
	// A cluster is a contiguous run of the input, so the clusters are substring
	// headers rather than freshly built strings — building one string per cluster
	// was the largest single source of allocations in markdown rendering.
	//
	// Nothing in ASCII extends a cluster, so every byte is a cluster and there is
	// no need to look at runes at all.
	if isASCII(text) {
		segments := make([]string, len(text))
		for index := 0; index < len(text); index++ {
			segments[index] = text[index : index+1]
		}
		return segments
	}

	var segments []string
	clusterStart := 0
	previous := rune(0)
	for index, current := range text {
		if index == clusterStart {
			previous = current
			continue
		}
		if extendsCluster(previous, current) {
			previous = current
			continue
		}
		segments = append(segments, text[clusterStart:index])
		clusterStart = index
		previous = current
	}
	return append(segments, text[clusterStart:])
}

// scanGraphemes visits each grapheme cluster without building the []string
// segmentGraphemes allocates (D230: the paint path measures and slices the same
// line several times per frame; the per-cluster slice was ~60% of a scroll
// frame's allocated bytes). Callers that only measure or slice a line use it.
// fn returns false to stop early.
func scanGraphemes(text string, fn func(segment string) bool) {
	if text == "" {
		return
	}
	if isASCII(text) {
		for index := 0; index < len(text); index++ {
			if !fn(text[index : index+1]) {
				return
			}
		}
		return
	}
	clusterStart := 0
	previous := rune(0)
	for index, current := range text {
		if index == clusterStart {
			previous = current
			continue
		}
		if extendsCluster(previous, current) {
			previous = current
			continue
		}
		if !fn(text[clusterStart:index]) {
			return
		}
		clusterStart = index
		previous = current
	}
	fn(text[clusterStart:])
}

// isASCII reports whether every byte is plain ASCII.
func isASCII(text string) bool {
	for index := 0; index < len(text); index++ {
		if text[index] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// extendsCluster reports whether r continues the grapheme cluster that ends
// with last.
func extendsCluster(last, r rune) bool {
	// Combining marks extend (Mn/Me and the spacing exceptions).
	if isCombiningMark(r) {
		return true
	}
	// Variation selectors extend.
	if r == 0xFE0E || r == 0xFE0F {
		return true
	}
	// A ZWJ joins the next character into the cluster.
	if r == 0x200D {
		return true
	}
	// Skin tone modifiers attach to emoji.
	if isSkinToneModifier(r) {
		return true
	}
	// Regional indicator pairs form flag emoji.
	if isRegionalIndicator(last) && isRegionalIndicator(r) {
		return true
	}
	// A virama joins the consonant cluster (indic scripts).
	if isVirama(last) {
		return true
	}
	// The character after a ZWJ joins the cluster (emoji sequences).
	if last == 0x200D {
		return true
	}
	return false
}

// isVirama covers the indic virama/sign-visarga separatives.
func isVirama(r rune) bool {
	switch r {
	case 0x094D, 0x09CD, 0x0A4D, 0x0ACD, 0x0B4D, 0x0BCD, 0x0C4D, 0x0CCD,
		0x0D4D, 0x0DCA, 0x0E4C, 0x0E4A, 0x0F84, 0x1039, 0x103A, 0x1714, 0x17D1:
		return true
	}
	return false
}

// isCombiningMark reports the Unicode Mark general category, matching
// upstream's markCharRegex (`\p{Mark}`).
func isCombiningMark(r rune) bool { return unicode.IsMark(r) }

func isSkinToneModifier(r rune) bool {
	return r >= 0x1F3FB && r <= 0x1F3FF
}

func isRegionalIndicator(r rune) bool {
	return r >= 0x1F1E6 && r <= 0x1F1FF
}

// isInTable reports whether r is in an inclusive range table. The tables are
// sorted, so a rune below the first range is an immediate miss: this keeps the
// common ASCII path out of the binary search (width computation is per rune,
// and width is recomputed for the whole content on every paint).
func isInTable(r rune, table [][2]rune) bool {
	if len(table) > 0 && r < table[0][0] {
		return false
	}
	low, high := 0, len(table)-1
	for low <= high {
		mid := (low + high) / 2
		switch {
		case r < table[mid][0]:
			high = mid - 1
		case r > table[mid][1]:
			low = mid + 1
		default:
			return true
		}
	}
	return false
}

// couldBeEmoji pre-filters segments before the full emoji check.
func couldBeEmoji(segment string) bool {
	first, size := utf8.DecodeRuneInString(segment)
	if size == 0 {
		return false
	}
	return (first >= 0x1F000 && first <= 0x1FBFF) ||
		(first >= 0x2300 && first <= 0x23FF) ||
		(first >= 0x2600 && first <= 0x27BF) ||
		(first >= 0x2B50 && first <= 0x2B55) ||
		strings.ContainsRune(segment, 0xFE0F) ||
		utf8.RuneCountInString(segment) > 2
}

// isPrintableASCII reports whether every byte is printable ASCII.
func isPrintableASCII(text string) bool {
	for index := 0; index < len(text); index++ {
		if text[index] < 0x20 || text[index] > 0x7e {
			return false
		}
	}
	return true
}

// graphemeWidth returns the terminal cell width of one grapheme cluster.
func graphemeWidth(segment string) int {
	if segment == "\t" {
		return 3
	}
	// Printable ASCII is one cell and never combining, an emoji, or wide. It is
	// by far the most common segment, and width is recomputed for the whole
	// content on every paint, so skip the Unicode tables for it.
	if len(segment) == 1 {
		if b := segment[0]; b >= 0x20 && b < 0x7F {
			return 1
		}
	}

	// Marks that terminals allocate cells for even without a base character
	// (upstream terminalSpacingMarkRegex tests the whole segment).
	if isTerminalSpacingMarkSegment(segment) {
		return utf8.RuneCountInString(segment)
	}

	// Zero-width clusters (ignorable code points, lone controls, marks).
	zeroWidth := true
	for _, r := range segment {
		if !isZeroWidthCodePoint(r) {
			zeroWidth = false
			break
		}
	}
	if zeroWidth {
		return 0
	}

	// Emoji (with the pre-filter) render double width.
	if couldBeEmoji(segment) && isRGIEmoji(segment) {
		return 2
	}

	// The base visible code point.
	base := stripLeadingNonPrinting(segment)
	if base == "" {
		return 0
	}
	baseFirst, baseSize := utf8.DecodeRuneInString(base)

	// Regional indicators render as full-width emoji even isolated.
	if isRegionalIndicator(baseFirst) {
		return 2
	}

	width := eastAsianWidth(baseFirst)

	// Trailing visible code points terminals may allocate cells for: indic
	// consonants after marks, halfwidth/fullwidth forms, Thai/Lao AM vowels.
	followsMark := false
	for _, char := range base[baseSize:] {
		switch {
		case isTerminalSpacingMark(char):
			width++
			followsMark = false
		case isCombiningMark(char):
			followsMark = true
		case !isNonPrintingCodePoint(char):
			if followsMark || (char >= 0xFF00 && char <= 0xFFEF) {
				width += eastAsianWidth(char)
			} else if char == 0x0E33 || char == 0x0EB3 {
				width++
			}
			followsMark = false
		}
	}
	return width
}

// eastAsianWidth returns 2 for wide/fullwidth runes, else 1.
func eastAsianWidth(r rune) int {
	kind := width.LookupRune(r).Kind()
	if kind == width.EastAsianWide || kind == width.EastAsianFullwidth {
		return 2
	}
	return 1
}

// isDefaultIgnorable reports the Unicode Default_Ignorable_Code_Point
// property, which Go exposes only as its components.
func isDefaultIgnorable(r rune) bool {
	if unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r) || unicode.Is(unicode.Variation_Selector, r) {
		return true
	}
	if !unicode.Is(unicode.Cf, r) {
		return false
	}
	// Default_Ignorable excludes White_Space, FFF9..FFFB and the
	// Prepended_Concatenation_Mark characters.
	return !unicode.Is(unicode.White_Space, r) &&
		!(r >= 0xFFF9 && r <= 0xFFFB) &&
		!unicode.Is(unicode.Prepended_Concatenation_Mark, r)
}

// isZeroWidthCodePoint covers upstream's zeroWidthRegex:
// Default_Ignorable_Code_Point | Control | Mark | Surrogate.
func isZeroWidthCodePoint(r rune) bool {
	return isDefaultIgnorable(r) || unicode.Is(unicode.Cc, r) ||
		unicode.Is(unicode.Mark, r) || unicode.Is(unicode.Cs, r)
}

// isNonPrintingCodePoint covers upstream's nonPrintingCharRegex:
// Default_Ignorable_Code_Point | Control | Format | Mark | Surrogate.
func isNonPrintingCodePoint(r rune) bool {
	return isDefaultIgnorable(r) || unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) ||
		unicode.Is(unicode.Mark, r) || unicode.Is(unicode.Cs, r)
}

// stripLeadingNonPrinting removes leading ignorable code points (upstream
// leadingNonPrintingRegex).
func stripLeadingNonPrinting(segment string) string {
	for index, r := range segment {
		if !isNonPrintingCodePoint(r) {
			return segment[index:]
		}
	}
	return ""
}

// isTerminalSpacingMark covers the spacing marks terminals allocate cells for:
// Unicode spacing marks minus the known non-spacing exceptions, plus the
// legacy wcwidth non-spacing exceptions that occupy cells.
// isTerminalSpacingMarkSegment reports whether every code point in the segment
// is a spacing mark the terminal allocates cells for (upstream
// terminalSpacingMarkRegex, `^(?:...)+$`).
func isTerminalSpacingMarkSegment(segment string) bool {
	if segment == "" {
		return false
	}
	for _, r := range segment {
		if !isTerminalSpacingMark(r) {
			return false
		}
	}
	return true
}

func isTerminalSpacingMark(r rune) bool {
	// Exceptions that do NOT occupy cells despite being spacing marks.
	if r == 0x1734 || r == 0x302E || r == 0x302F {
		return false
	}
	// Non-spacing exceptions that DO occupy cells.
	switch r {
	case 0x065F, 0x0F7F, 0x102B, 0x102C, 0x1031, 0x1033, 0x1034, 0x1035,
		0x1038, 0x103A, 0x103B, 0x103C, 0x103D, 0x103E:
		return true
	}
	// Unicode spacing marks (Mc).
	return unicode.Is(unicode.Mc, r)
}

// widthCache caches widths for non-ASCII strings.
// widthCacheSize is the production cap.
const widthCacheSize = 32768

// widthCacheCap is the effective cap. It is a variable so the eviction test can
// exercise a small cache; production never changes it.
var widthCacheCap = widthCacheSize

// resetWidthCacheForTest empties the cache and its counter (test seam).
func resetWidthCacheForTest() {
	widthCache.Range(func(key, _ any) bool {
		widthCache.Delete(key)
		return true
	})
	widthCacheEntries.Store(0)
}

// widthCacheEvictBatch is how many entries a full cache drops at once. Evicting
// (or even counting entries) one at a time meant a full sync.Map range per miss:
// ~8% of a warm long-transcript frame went into the map iterator.
const widthCacheEvictBatch = 64

// widthCache memoizes non-ASCII widths. Stage 4 removed its mutex: sync.Map is
// safe for the read-mostly, concurrent use from rendering and non-UI callers.
var widthCache sync.Map

// widthCacheEntries approximates the cache size so a miss does not have to
// count the map; the bound is best-effort under concurrent eviction.
var widthCacheEntries atomic.Int64

// widthCacheEntryCount counts the memoized entries.
func widthCacheEntryCount() int {
	count := 0
	widthCache.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// widthCacheStore memoizes a width and evicts a batch of arbitrary entries when
// the cache is over its size.
func widthCacheStore(text string, width int) {
	if _, loaded := widthCache.LoadOrStore(text, width); loaded {
		return
	}
	if widthCacheEntries.Add(1) <= int64(widthCacheCap) {
		return
	}
	evicted := 0
	widthCache.Range(func(key, _ any) bool {
		widthCache.Delete(key)
		evicted++
		return evicted < widthCacheEvictBatch
	})
	widthCacheEntries.Add(int64(-evicted))
}

// VisibleWidth returns the width of a string in terminal columns: tabs count
// three, terminal sequences are invisible, grapheme clusters use their
// terminal presentation width.
func VisibleWidth(text string) int {
	if text == "" {
		return 0
	}
	if isPrintableASCII(text) {
		return len(text)
	}

	if cached, ok := widthCache.Load(text); ok {
		return cached.(int)
	}
	// Styled ASCII (SGR-colored output and diffs) is the common per-line case in
	// a Box's background pass: measure it without allocating a stripped copy or
	// walking graphemes. Anything else falls through to the general path.
	if width, ok := styledASCIIWidth(text); ok {
		widthCacheStore(text, width)
		return width
	}

	clean := text
	if strings.Contains(clean, "\t") {
		clean = strings.ReplaceAll(clean, "\t", "   ")
	}
	if strings.Contains(clean, "\x1b") {
		clean = StripTerminalSequences(clean)
	}
	// Escapes and tabs are gone. Terminal text is overwhelmingly printable
	// ASCII, whose width is its byte count, and styled lines never reach the
	// isPrintableASCII fast path above. Short-circuiting here keeps the grapheme
	// walk off the per-frame path (D164): wrapping, padding and painting measure
	// every visible line of every frame, and the walk costs a decode plus two
	// range searches per character.
	if isPrintableASCII(clean) {
		widthCacheStore(text, len(clean))
		return len(clean)
	}
	total := 0
	for _, segment := range segmentGraphemes(clean) {
		total += graphemeWidth(segment)
	}

	widthCacheStore(text, total)
	return total
}

// StripTerminalSequences removes ANSI, OSC, and APC control sequences while
// preserving visible text.
func StripTerminalSequences(text string) string {
	if !strings.Contains(text, "\x1b") {
		return text
	}
	var result strings.Builder
	index := 0
	for index < len(text) {
		if code, length := ExtractANSICode(text, index); code != "" {
			index += length
			continue
		}
		result.WriteByte(text[index])
		index++
	}
	return result.String()
}

// ExtractANSICode returns the terminal escape sequence at pos, if any:
// CSI styling/cursor codes, OSC hyperlinks and prompts, APC markers.
// The length covers the whole sequence.
func ExtractANSICode(text string, pos int) (string, int) {
	if pos >= len(text) || text[pos] != '\x1b' {
		return "", 0
	}
	if pos+1 >= len(text) {
		return "", 0
	}
	switch text[pos+1] {
	case '[': // CSI: ESC [ ... final byte in m G K H J
		j := pos + 2
		for j < len(text) {
			c := text[j]
			if c == 'm' || c == 'G' || c == 'K' || c == 'H' || c == 'J' {
				return text[pos : j+1], j + 1 - pos
			}
			j++
		}
		return "", 0
	case ']', '_': // OSC and APC: terminated by BEL or ESC \
		j := pos + 2
		for j < len(text) {
			if text[j] == '\x07' {
				return text[pos : j+1], j + 1 - pos
			}
			if text[j] == '\x1b' && j+1 < len(text) && text[j+1] == '\\' {
				return text[pos : j+2], j + 2 - pos
			}
			j++
		}
		return "", 0
	}
	return "", 0
}

// ---- character classification helpers (src/utils.ts) ----

// isWhitespaceRune mirrors JS /\s/ for the practical set.
func isWhitespaceRune(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\v', '\f', '\r', 0x00A0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// IsWhitespaceChar reports whether a string is entirely whitespace.
func IsWhitespaceChar(char string) bool {
	for _, r := range char {
		if !isWhitespaceRune(r) {
			return false
		}
	}
	return len(char) > 0
}

func isLetterRune(r rune) bool { return unicode.IsLetter(r) }

func isDigitRune(r rune) bool { return unicode.IsDigit(r) }

func isMarkRune(r rune) bool { return unicode.IsMark(r) }

// styledASCIIWidth returns the display width of a string that is ASCII apart
// from ANSI CSI escape sequences, without allocating a stripped copy or walking
// graphemes. It reports false for tabs, non-ASCII bytes, or non-CSI escapes, so
// the general path handles those. Used by VisibleWidth's fast path; the box
// background pass measures every line of a long output.
func styledASCIIWidth(text string) (int, bool) {
	width := 0
	sawEscape := false
	for i := 0; i < len(text); {
		c := text[i]
		if c == 0x1b {
			if i+1 >= len(text) || text[i+1] != '[' {
				return 0, false
			}
			sawEscape = true
			j := i + 2
			for j < len(text) && (text[j] < 0x40 || text[j] > 0x7e) {
				if text[j] == 0x1b {
					return 0, false
				}
				j++
			}
			if j >= len(text) {
				return 0, false
			}
			i = j + 1
			continue
		}
		if c < 0x20 || c > 0x7e {
			return 0, false
		}
		width++
		i++
	}
	if !sawEscape {
		return 0, false
	}
	return width, true
}
