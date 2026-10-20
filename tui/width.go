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
	var grouper graphemeGrouper
	clusterStart := 0
	for index, current := range text {
		if index == clusterStart {
			grouper.start(current)
			continue
		}
		if grouper.extend(current) {
			continue
		}
		segments = append(segments, text[clusterStart:index])
		clusterStart = index
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
	var grouper graphemeGrouper
	for index, current := range text {
		if index == clusterStart {
			grouper.start(current)
			continue
		}
		if grouper.extend(current) {
			continue
		}
		if !fn(text[clusterStart:index]) {
			return
		}
		clusterStart = index
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

// graphemeGrouper decides grapheme cluster boundaries: the UAX #29 rules the
// width math depends on, because some widths are only correct for the cluster
// Intl.Segmenter would produce (a trailing Hangul jamo, a whole flag, an emoji
// ZWJ sequence).
type graphemeGrouper struct {
	previous     rune
	previousInfo runeInfo
	// tailBase is the cluster's last rune that is not an Extend. Whether it is
	// an extended pictograph is only asked when a ZWJ arrives, so the emoji table
	// is not searched per rune.
	tailBase rune
	// tailOnlyExtend reports that every rune after tailBase is an Extend, which
	// is what the ExtPict Extend* ZWJ pattern of rule GB11 requires.
	tailOnlyExtend bool
	// joinAfterZWJ reports that the cluster ends in ExtPict Extend* ZWJ, so an
	// extended pictograph joins it (rule GB11).
	joinAfterZWJ bool
	// hangul is the Hangul syllable type of previous (rules GB6-GB8).
	hangul hangulType
	// regionalIndicators counts the regional indicators in the cluster, which
	// pair up (rules GB12-GB13).
	regionalIndicators int
}

// runeInfo is the per-rune classification of the boundary rules. It is
// computed once per rune instead of once per neighbouring pair.
type runeInfo struct {
	control bool
	prepend bool
	linker  bool
}

func classifyGraphemeRune(r rune) runeInfo {
	return runeInfo{
		control: isGraphemeControl(r),
		prepend: isPrepend(r),
		linker:  isLinker(r),
	}
}

// start begins a cluster at r.
func (g *graphemeGrouper) start(r rune) { g.begin(r, classifyGraphemeRune(r)) }

// begin begins a cluster at r whose classification is already known.
func (g *graphemeGrouper) begin(r rune, info runeInfo) {
	g.previous = r
	g.previousInfo = info
	g.tailBase = r
	g.tailOnlyExtend = true
	g.joinAfterZWJ = false
	g.hangul = hangulTypeOf(r)
	if isRegionalIndicator(r) {
		g.regionalIndicators = 1
	} else {
		g.regionalIndicators = 0
	}
}

// extend reports whether r continues the current cluster.
func (g *graphemeGrouper) extend(r rune) bool {
	info := classifyGraphemeRune(r)
	previous := g.previous
	// GB3: CR × LF.
	if previous == '\r' && r == '\n' {
		g.append(r, info)
		return true
	}
	// GB4/GB5: controls break on both sides.
	if g.previousInfo.control || info.control {
		g.begin(r, info)
		return false
	}
	// GB9: × (Extend | ZWJ).
	if isGraphemeExtend(r) {
		g.append(r, info)
		return true
	}
	// GB9a: × SpacingMark.
	if isGraphemeSpacingMark(r) {
		g.append(r, info)
		return true
	}
	// GB11: ExtPict Extend* ZWJ × ExtPict. A ZWJ after a non-pictograph does not
	// join what follows it.
	if g.joinAfterZWJ && isExtendedPictographic(r) {
		g.append(r, info)
		return true
	}
	// GB9b: Prepend ×.
	if g.previousInfo.prepend {
		g.append(r, info)
		return true
	}
	// GB6-GB8: Hangul syllable sequences.
	if hangulJoins(g.hangul, hangulTypeOf(r)) {
		g.append(r, info)
		return true
	}
	// GB12/GB13: regional indicators pair up, so a third one starts a new flag.
	if isRegionalIndicator(r) && g.regionalIndicators&1 == 1 {
		g.append(r, info)
		return true
	}
	// A linker joins the following consonant into the same cluster (UAX #29
	// GB9c): a digit, space or letter of another script starts a new cluster.
	if g.previousInfo.linker && linkerJoinsConsonant(previous, r) {
		g.append(r, info)
		return true
	}
	g.begin(r, info)
	return false
}

// append records r as part of the current cluster.
func (g *graphemeGrouper) append(r rune, info runeInfo) {
	switch {
	case r == 0x200D:
		// The ZWJ closes the ExtPict Extend* run it may join from, so a second
		// ZWJ in the same cluster does not join again.
		g.joinAfterZWJ = g.tailOnlyExtend && isExtendedPictographic(g.tailBase)
		g.tailOnlyExtend = false
	case isGraphemeExtend(r):
		// Extending code points keep the base a later ZWJ would test.
		g.joinAfterZWJ = false
	case isGraphemeSpacingMark(r):
		// A spacing mark is not an Extend, so the GB11 chain starts over at it.
		g.joinAfterZWJ = false
		g.tailBase = r
		g.tailOnlyExtend = true
	default:
		g.joinAfterZWJ = false
		g.tailBase = r
		g.tailOnlyExtend = true
	}
	if isRegionalIndicator(r) {
		g.regionalIndicators++
	} else {
		g.regionalIndicators = 0
	}
	g.hangul = hangulTypeOf(r)
	g.previous = r
	g.previousInfo = info
}

// graphemeControlRanges are the UAX #29 Control code points: Cc, the Cf
// format characters except ZWNJ and ZWJ, Zl and Zp, and the unassigned code
// points in the tags block. Prepend characters are excluded (rule GB9b attaches
// them) as are the tag characters and the variation-selector supplement (both
// Extend). The table is checked against the Unicode predicates by
// TestGraphemeControlRanges.
var graphemeControlRanges = [][2]rune{
	{0x0, 0x1F},
	{0x7F, 0x9F},
	{0xAD, 0xAD},
	{0x61C, 0x61C},
	{0x180E, 0x180E},
	{0x200B, 0x200B},
	{0x200E, 0x200F},
	{0x2028, 0x202E},
	{0x2060, 0x2064},
	{0x2066, 0x206F},
	{0xFEFF, 0xFEFF},
	{0xFFF9, 0xFFFB},
	{0x13430, 0x1343F},
	{0x1BCA0, 0x1BCA3},
	{0x1D173, 0x1D17A},
	{0xE0000, 0xE001F},
	{0xE0080, 0xE00FF},
	{0xE01F0, 0xE0FFF},
}

// isGraphemeControl reports the UAX #29 Control class.
func isGraphemeControl(r rune) bool { return isInTable(r, graphemeControlRanges) }

// isGraphemeExtend reports the UAX #29 Extend class: marks, variation
// selectors, emoji modifiers, ZWNJ and tag characters.
func isGraphemeExtend(r rune) bool {
	if unicode.IsMark(r) {
		return true
	}
	if unicode.Is(unicode.Variation_Selector, r) || isSkinToneModifier(r) {
		return true
	}
	if r == 0x200C || r == 0x200D {
		return true
	}
	return r >= 0xE0020 && r <= 0xE007F
}

// prependRanges are the UAX #29 Prepend code points. The
// Prepended_Concatenation_Mark property Go exposes omits the Malayalam dot reph
// (U+0D4E) in the Unicode version this build ships, so the class is spelled out.
var prependRanges = [][2]rune{
	{0x0600, 0x0605}, {0x06DD, 0x06DD}, {0x070F, 0x070F}, {0x0890, 0x0891},
	{0x08E2, 0x08E2}, {0x0D4E, 0x0D4E}, {0x110BD, 0x110BD}, {0x110CD, 0x110CD},
	{0x111C2, 0x111C3}, {0x1193F, 0x1193F}, {0x11941, 0x11941},
	{0x11A3A, 0x11A3A}, {0x11A84, 0x11A89}, {0x11D46, 0x11D46}, {0x11F02, 0x11F02},
}

// isGraphemeSpacingMark reports the two spacing marks that are letters rather
// than Marks (rule GB9a): the Thai and Lao AM vowels. Every other spacing mark
// is a Mark, which rule GB9 already joins.
func isGraphemeSpacingMark(r rune) bool { return r == 0x0E33 || r == 0x0EB3 }

// isPrepend reports the UAX #29 Prepend class.
func isPrepend(r rune) bool { return isInTable(r, prependRanges) }

// isExtendedPictographic reports the Unicode Extended_Pictographic property
// (the set is generated into the emoji table). It is what makes a ZWJ join the
// code point after it (rule GB11).
func isExtendedPictographic(r rune) bool { return isInTable(r, extendedPictographicRanges) }

// hangulType is a Hangul_Syllable_Type value.
type hangulType uint8

const (
	hangulNone hangulType = iota
	hangulL
	hangulV
	hangulT
	hangulLV
	hangulLVT
)

// hangulTypeOf classifies a code point for the Hangul rules GB6-GB8.
func hangulTypeOf(r rune) hangulType {
	switch {
	case r >= 0x1100 && r <= 0x115F, r >= 0xA960 && r <= 0xA97C:
		return hangulL
	case r >= 0x1160 && r <= 0x11A7, r >= 0xD7B0 && r <= 0xD7C6:
		return hangulV
	case r >= 0x11A8 && r <= 0x11FF, r >= 0xD7CB && r <= 0xD7FB:
		return hangulT
	case r >= 0xAC00 && r <= 0xD7A3:
		if (r-0xAC00)%28 == 0 {
			return hangulLV
		}
		return hangulLVT
	}
	return hangulNone
}

// hangulJoins applies rules GB6-GB8.
func hangulJoins(previous, next hangulType) bool {
	switch previous {
	case hangulL:
		return next == hangulL || next == hangulV || next == hangulLV || next == hangulLVT
	case hangulLV, hangulV:
		return next == hangulV || next == hangulT
	case hangulLVT, hangulT:
		return next == hangulT
	}
	return false
}

// indicLinkerScripts maps each virama that joins a following consonant (UAX #29
// InCB=Linker) to the script block its consonants live in. Intl.Segmenter also
// joins Myanmar U+1039 and Tai Tham U+1A60. Thai, Lao, Tibetan, Khmer, Tamil,
// Kannada, Sinhala and Gurmukhi viramas do not join: their consonant starts a
// new cluster.
func indicLinkerBlock(linker rune) (block [2]rune, ok bool) {
	switch linker {
	case 0x094D:
		return [2]rune{0x0900, 0x097F}, true // Devanagari
	case 0x09CD:
		return [2]rune{0x0980, 0x09FF}, true // Bengali
	case 0x0ACD:
		return [2]rune{0x0A80, 0x0AFF}, true // Gujarati
	case 0x0B4D:
		return [2]rune{0x0B00, 0x0B7F}, true // Oriya
	case 0x0C4D:
		return [2]rune{0x0C00, 0x0C7F}, true // Telugu
	case 0x0D4D:
		return [2]rune{0x0D00, 0x0D7F}, true // Malayalam
	case 0x1039:
		return [2]rune{0x1000, 0x109F}, true // Myanmar
	case 0x1A60:
		return [2]rune{0x1A20, 0x1AAF}, true // Tai Tham
	}
	return block, false
}

// isLinker reports whether r is one of the linker viramas (rule GB9c).
func isLinker(r rune) bool {
	_, ok := indicLinkerBlock(r)
	return ok
}

// linkerJoinsConsonant reports whether r is a consonant of the linker's script,
// which is the rune a virama joins (rule GB9c). A digit, space, or a letter from
// another script starts a new cluster.
func linkerJoinsConsonant(linker rune, r rune) bool {
	block, ok := indicLinkerBlock(linker)
	return ok && r >= block[0] && r <= block[1] && unicode.IsLetter(r)
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
