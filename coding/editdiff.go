package coding

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dat267/pier/internal/jsdiff"
	"golang.org/x/text/unicode/norm"
)

// Port of core/tools/edit-diff.ts: line-ending handling, fuzzy matching,
// exact-replacement application, and diff generation.

// NormalizeToLF rewrites CRLF and CR to LF.
func NormalizeToLF(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

// SplitBom splits a leading UTF-8 BOM from decoded text (utils/text.ts).
// JS slice(1) removes one UTF-16 code unit; the Go port removes the full
// rune (3 bytes for the BOM).
func SplitBom(content string) (bom string, text string) {
	if strings.HasPrefix(content, "\uFEFF") {
		return "\uFEFF", content[len("\uFEFF"):]
	}
	return "", content
}

// NormalizeForFuzzyMatch applies the progressive fuzzy-match normalization:
// NFKC, per-line trailing-whitespace strip, smart quotes → ASCII, unicode
// dashes → '-', special spaces → ' '.
func NormalizeForFuzzyMatch(text string) string {
	normalized := norm.NFKC.String(text)
	lines := strings.Split(normalized, "\n")
	for i, line := range lines {
		lines[i] = trimJSTrailing(line)
	}
	normalized = strings.Join(lines, "\n")

	replacements := []struct{ from, to string }{
		{"\u2018", "'"}, {"\u2019", "'"}, {"\u201A", "'"}, {"\u201B", "'"},
		{"\u201C", "\""}, {"\u201D", "\""}, {"\u201E", "\""}, {"\u201F", "\""},
		{"\u2010", "-"}, {"\u2011", "-"}, {"\u2012", "-"}, {"\u2013", "-"},
		{"\u2014", "-"}, {"\u2015", "-"}, {"\u2212", "-"},
		{"\u00A0", " "}, {"\u2002", " "}, {"\u2003", " "}, {"\u2004", " "},
		{"\u2005", " "}, {"\u2006", " "}, {"\u2007", " "}, {"\u2008", " "},
		{"\u2009", " "}, {"\u200A", " "}, {"\u202F", " "}, {"\u205F", " "},
		{"\u3000", " "},
	}
	for _, r := range replacements {
		normalized = strings.ReplaceAll(normalized, r.from, r.to)
	}
	return normalized
}

// trimJSTrailing strips JS-whitespace trailing characters.
func trimJSTrailing(line string) string {
	return strings.TrimRight(line, " \t\x0b\x0c\r\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff")
}

// splitLinesWithEndings splits preserving line terminators (JS
// content.match(/[^\n]*\n|[^\n]+/g)).
func splitLinesWithEndings(content string) []string {
	var out []string
	start := 0
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			out = append(out, content[start:i+1])
			start = i + 1
		}
	}
	if start < len(content) {
		out = append(out, content[start:])
	}
	return out
}

// lineSpan is a line's [start, end) offsets.
type lineSpan struct {
	start int
	end   int
}

func getLineSpans(content string) []lineSpan {
	offset := 0
	var spans []lineSpan
	for _, line := range splitLinesWithEndings(content) {
		spans = append(spans, lineSpan{start: offset, end: offset + len(line)})
		offset += len(line)
	}
	return spans
}

// textReplacement is one positioned replacement.
type textReplacement struct {
	matchIndex  int
	matchLength int
	newText     string
}

// getReplacementLineRange maps a replacement onto touched line indices.
func getReplacementLineRange(lines []lineSpan, replacement textReplacement) (startLine, endLine int, err error) {
	replacementStart := replacement.matchIndex
	replacementEnd := replacement.matchIndex + replacement.matchLength

	startLine = -1
	for i, line := range lines {
		if replacementStart >= line.start && replacementStart < line.end {
			startLine = i
			break
		}
	}
	if startLine == -1 {
		return 0, 0, fmt.Errorf("Replacement range is outside the base content.")
	}
	endLine = startLine
	for endLine < len(lines) && lines[endLine].end < replacementEnd {
		endLine++
	}
	if endLine >= len(lines) {
		return 0, 0, fmt.Errorf("Replacement range is outside the base content.")
	}
	return startLine, endLine + 1, nil
}

// applyReplacements applies replacements (matched against baseContent with
// an offset) in reverse order so offsets stay stable.
func applyReplacements(content string, replacements []textReplacement, offset int) string {
	result := content
	for _, replacement := range slices.Backward(replacements) {

		matchIndex := replacement.matchIndex - offset
		result = result[:matchIndex] + replacement.newText + result[matchIndex+replacement.matchLength:]
	}
	return result
}

// widenReplacementsToLines turns replacements matched against baseContent (a
// normalized view) into replacements of originalContent that keep unchanged
// lines from the original. Each replacement is widened to the lines it touches,
// and those lines are rewritten from the normalized base; all other lines keep
// their original text.
func widenReplacementsToLines(originalContent, baseContent string, replacements []textReplacement) ([]textReplacement, error) {
	originalLines := getLineSpans(originalContent)
	baseLines := getLineSpans(baseContent)
	if len(originalLines) != len(baseLines) {
		return nil, fmt.Errorf("Cannot preserve unchanged lines because the base content has a different line count.")
	}

	type group struct {
		startLine, endLine int
		replacements       []textReplacement
	}
	sorted := append([]textReplacement{}, replacements...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].matchIndex < sorted[j-1].matchIndex; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	var groups []group
	for _, replacement := range sorted {
		startLine, endLine, err := getReplacementLineRange(baseLines, replacement)
		if err != nil {
			return nil, err
		}
		if len(groups) > 0 {
			current := &groups[len(groups)-1]
			if startLine < current.endLine {
				if endLine > current.endLine {
					current.endLine = endLine
				}
				current.replacements = append(current.replacements, replacement)
				continue
			}
		}
		groups = append(groups, group{startLine: startLine, endLine: endLine, replacements: []textReplacement{replacement}})
	}

	widened := make([]textReplacement, 0, len(groups))
	for _, g := range groups {
		groupStartOffset := baseLines[g.startLine].start
		groupEndOffset := baseLines[g.endLine-1].end
		originalStart := originalLines[g.startLine].start
		widened = append(widened, textReplacement{
			matchIndex:  originalStart,
			matchLength: originalLines[g.endLine-1].end - originalStart,
			newText:     applyReplacements(baseContent[groupStartOffset:groupEndOffset], g.replacements, groupStartOffset),
		})
	}
	return widened, nil
}

// applyReplacementsToOriginal applies replacements matched in
// NormalizeToLF(originalContent) to originalContent itself (pi v1.5.2
// core/tools/edit-diff.ts applyReplacementsToOriginal). Text outside the
// replaced ranges keeps its bytes, including its line endings; line breaks in
// the new text get the ending of the line the range starts on. The result,
// normalized to LF, equals the replacements applied to the normalized content.
func applyReplacementsToOriginal(originalContent string, replacements []textReplacement) string {
	// Only CRLF changes length when normalizing: the LF of the n-th CRLF (at
	// original offset p) sits at p - n in the normalized text. Adding the number
	// of CRLFs before a normalized offset gives the original offset, and a range
	// that covers that LF covers the whole CRLF.
	crlfBefore := 0
	nextCrlf := strings.Index(originalContent, "\r\n")
	toOriginal := func(offset int) int {
		for nextCrlf != -1 && nextCrlf-crlfBefore < offset {
			crlfBefore++
			if found := strings.Index(originalContent[nextCrlf+2:], "\r\n"); found == -1 {
				nextCrlf = -1
			} else {
				nextCrlf += 2 + found
			}
		}
		return offset + crlfBefore
	}

	parts := make([]string, 0, len(replacements)*2+1)
	endsWithCr := false
	push := func(part string) {
		if part == "" {
			return
		}
		// A CR before a seam and an LF after it would merge into one CRLF line
		// break, for example a kept lone CR followed by inserted text that starts
		// with LF, or both sides of a deleted range. Adding an LF turns the CR
		// into a CRLF of its own, so the written file has the breaks the diff
		// shows.
		if endsWithCr && part[0] == '\n' {
			parts = append(parts, "\n")
		}
		parts = append(parts, part)
		endsWithCr = part[len(part)-1] == '\r'
	}
	position := 0
	for _, replacement := range replacements {
		start := toOriginal(replacement.matchIndex)
		push(originalContent[position:start])
		ending := getLineEndingAt(originalContent, start)
		if ending == "\n" {
			push(replacement.newText)
		} else {
			push(strings.ReplaceAll(replacement.newText, "\n", ending))
		}
		position = toOriginal(replacement.matchIndex + replacement.matchLength)
	}
	push(originalContent[position:])
	return strings.Join(parts, "")
}

// getLineEndingAt reports the line ending of the line containing offset: its own
// LF or CRLF, or that of the line before for an unterminated last line. A lone CR
// only counts in files without any LF (classic Mac line endings); elsewhere it is
// more likely a control character inside a line.
func getLineEndingAt(content string, offset int) string {
	lineFeed := -1
	if found := strings.Index(content[offset:], "\n"); found != -1 {
		lineFeed = offset + found
	} else if found := strings.LastIndex(content[:offset], "\n"); found != -1 {
		lineFeed = found
	}
	if lineFeed == -1 {
		if strings.Contains(content, "\r") {
			return "\r"
		}
		return "\n"
	}
	if lineFeed > 0 && content[lineFeed-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

// FuzzyMatchResult is the fuzzyFindText outcome.
type FuzzyMatchResult struct {
	Found                 bool
	Index                 int
	MatchLength           int
	UsedFuzzyMatch        bool
	ContentForReplacement string
}

// FuzzyFindText finds oldText in content, exact first, then fuzzy (in
// normalized space).
func FuzzyFindText(content string, oldText string) FuzzyMatchResult {
	if exactIndex := strings.Index(content, oldText); exactIndex != -1 {
		return FuzzyMatchResult{
			Found: true, Index: exactIndex, MatchLength: len(oldText),
			UsedFuzzyMatch: false, ContentForReplacement: content,
		}
	}
	fuzzyContent := NormalizeForFuzzyMatch(content)
	fuzzyOldText := NormalizeForFuzzyMatch(oldText)
	fuzzyIndex := strings.Index(fuzzyContent, fuzzyOldText)
	if fuzzyIndex == -1 {
		return FuzzyMatchResult{Found: false, Index: -1, ContentForReplacement: content}
	}
	return FuzzyMatchResult{
		Found: true, Index: fuzzyIndex, MatchLength: len(fuzzyOldText),
		UsedFuzzyMatch: true, ContentForReplacement: fuzzyContent,
	}
}

func countOccurrences(content string, oldText string) int {
	fuzzyContent := NormalizeForFuzzyMatch(content)
	fuzzyOldText := NormalizeForFuzzyMatch(oldText)
	if fuzzyOldText == "" {
		return JSLength(fuzzyContent) - 1
	}
	return strings.Count(fuzzyContent, fuzzyOldText)
}

// Edit is one replacement.
type Edit struct {
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

// AppliedEditsResult is the applyEditsToNormalizedContent outcome.
type AppliedEditsResult struct {
	BaseContent string
	NewContent  string
	// Replacements turn BaseContent into NewContent, in BaseContent offsets.
	Replacements []textReplacement
}

func getNotFoundError(path string, editIndex, totalEdits int) error {
	if totalEdits == 1 {
		return fmt.Errorf("Could not find the exact text in %s. The old text must match exactly including all whitespace and newlines.", path)
	}
	return fmt.Errorf("Could not find edits[%d] in %s. The oldText must match exactly including all whitespace and newlines.", editIndex, path)
}

func getDuplicateError(path string, editIndex, totalEdits, occurrences int) error {
	if totalEdits == 1 {
		return fmt.Errorf("Found %d occurrences of the text in %s. The text must be unique. Please provide more context to make it unique.", occurrences, path)
	}
	return fmt.Errorf("Found %d occurrences of edits[%d] in %s. Each oldText must be unique. Please provide more context to make it unique.", occurrences, editIndex, path)
}

func getEmptyOldTextError(path string, editIndex, totalEdits int) error {
	if totalEdits == 1 {
		return fmt.Errorf("oldText must not be empty in %s.", path)
	}
	return fmt.Errorf("edits[%d].oldText must not be empty in %s.", editIndex, path)
}

func getNoChangeError(path string, totalEdits int) error {
	if totalEdits == 1 {
		return fmt.Errorf("No changes made to %s. The replacement produced identical content. This might indicate an issue with special characters or the text not existing as expected.", path)
	}
	return fmt.Errorf("No changes made to %s. The replacements produced identical content.", path)
}

// ApplyEditsToNormalizedContent applies one or more exact-text replacements
// to LF-normalized content. All edits match the same original content;
// replacements apply in reverse order so offsets remain stable. Fuzzy
// matches run in normalized space and overlay line-level changes onto the
// original so unchanged line blocks keep their original bytes.
func ApplyEditsToNormalizedContent(normalizedContent string, edits []Edit, path string) (AppliedEditsResult, error) {
	normalizedEdits := make([]Edit, 0, len(edits))
	for _, edit := range edits {
		normalizedEdits = append(normalizedEdits, Edit{
			OldText: NormalizeToLF(edit.OldText),
			NewText: NormalizeToLF(edit.NewText),
		})
	}

	for i, edit := range normalizedEdits {
		if len(edit.OldText) == 0 {
			return AppliedEditsResult{}, getEmptyOldTextError(path, i, len(normalizedEdits))
		}
	}

	initialMatches := make([]FuzzyMatchResult, len(normalizedEdits))
	usedFuzzyMatch := false
	for i, edit := range normalizedEdits {
		initialMatches[i] = FuzzyFindText(normalizedContent, edit.OldText)
		if initialMatches[i].UsedFuzzyMatch {
			usedFuzzyMatch = true
		}
	}
	replacementBaseContent := normalizedContent
	if usedFuzzyMatch {
		replacementBaseContent = NormalizeForFuzzyMatch(normalizedContent)
	}

	matchedEdits := make([]matchedEdit, 0, len(normalizedEdits))
	for i, edit := range normalizedEdits {
		matchResult := FuzzyFindText(replacementBaseContent, edit.OldText)
		if !matchResult.Found {
			return AppliedEditsResult{}, getNotFoundError(path, i, len(normalizedEdits))
		}
		occurrences := countOccurrences(replacementBaseContent, edit.OldText)
		if occurrences > 1 {
			return AppliedEditsResult{}, getDuplicateError(path, i, len(normalizedEdits), occurrences)
		}
		matchedEdits = append(matchedEdits, matchedEdit{
			editIndex: i, matchIndex: matchResult.Index, matchLength: matchResult.MatchLength, newText: edit.NewText,
		})
	}

	// Upstream sorts matched edits by match index before the overlap check.
	for i := 1; i < len(matchedEdits); i++ {
		for j := i; j > 0 && matchedEdits[j].matchIndex < matchedEdits[j-1].matchIndex; j-- {
			matchedEdits[j], matchedEdits[j-1] = matchedEdits[j-1], matchedEdits[j]
		}
	}
	for i := 1; i < len(matchedEdits); i++ {
		previous, current := matchedEdits[i-1], matchedEdits[i]
		if previous.matchIndex+previous.matchLength > current.matchIndex {
			return AppliedEditsResult{}, fmt.Errorf(
				"edits[%d] and edits[%d] overlap in %s. Merge them into one edit or target disjoint regions.",
				previous.editIndex, current.editIndex, path)
		}
	}

	baseContent := normalizedContent
	var replacements []textReplacement
	if usedFuzzyMatch {
		widened, err := widenReplacementsToLines(normalizedContent, replacementBaseContent, toReplacements(matchedEdits))
		if err != nil {
			return AppliedEditsResult{}, err
		}
		replacements = widened
	} else {
		replacements = toReplacements(matchedEdits)
	}
	newContent := applyReplacements(baseContent, replacements, 0)

	if baseContent == newContent {
		return AppliedEditsResult{}, getNoChangeError(path, len(normalizedEdits))
	}
	return AppliedEditsResult{BaseContent: baseContent, NewContent: newContent, Replacements: replacements}, nil
}

type matchedEdit struct {
	editIndex   int
	matchIndex  int
	matchLength int
	newText     string
}

func toReplacements(matched []matchedEdit) []textReplacement {
	out := make([]textReplacement, 0, len(matched))
	for _, m := range matched {
		out = append(out, textReplacement{matchIndex: m.matchIndex, matchLength: m.matchLength, newText: m.newText})
	}
	return out
}

// GenerateUnifiedPatch builds a unified diff patch (port of
// generateUnifiedPatch -> jsdiff 8.0.4 createTwoFilesPatch with
// FILE_HEADERS_ONLY). Lines are newline-carrying tokens, so a final line
// without a newline differs from one with it and gets the standard
// "\ No newline at end of file" marker; hunks group like structuredPatch
// (an unchanged run of at most 2*context lines joins two hunks).
func GenerateUnifiedPatch(path, oldContent, newContent string, contextLines int) string {
	if contextLines < 0 {
		// jsdiff's structuredPatch has no default for a negative context; the
		// port keeps the display default rather than reproducing its slice
		// quirks. Zero is meaningful (one hunk per change, no context).
		contextLines = 4
	}
	oldTokens := splitPatchTokens(oldContent)
	newTokens := splitPatchTokens(newContent)
	// jsdiff diffs newline-carrying tokens with its Myers search; the LCS used by
	// the display diff picks different alignments for repeated lines.
	ops := patchOpsFromComponents(jsdiff.DiffComponents(oldTokens, newTokens, func(a, b string) bool { return a == b }))
	return formatPatchHunks(path, buildPatchHunks(oldTokens, newTokens, ops, contextLines))
}

// formatPatchHunks renders structured hunks as a unified patch with the file
// headers jsdiff's FILE_HEADERS_ONLY keeps (`--- path`, `+++ path`).
func formatPatchHunks(path string, hunks []patchHunk) string {
	var out strings.Builder
	out.WriteString("--- " + path + "\n")
	out.WriteString("+++ " + path + "\n")
	for _, hunk := range hunks {
		oldStart, newStart := hunk.oldStart, hunk.newStart
		// Unified diff quirk: a zero-length range starts one line earlier.
		if hunk.oldLines == 0 {
			oldStart--
		}
		if hunk.newLines == 0 {
			newStart--
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", oldStart, hunk.oldLines, newStart, hunk.newLines)
		for _, line := range hunk.lines {
			// Each entry still carries its newline; a line without one gets the
			// marker after it (upstream structuredPatch step 2).
			if before, ok := strings.CutSuffix(line, "\n"); ok {
				out.WriteString(before)
			} else {
				out.WriteString(line)
				out.WriteString("\n\\ No newline at end of file")
			}
			out.WriteString("\n")
		}
	}
	return out.String()
}

// splitPatchTokens splits content into lines that keep their terminator, the
// jsdiff diffLines token model: the last token has no terminator only when the
// content does not end with one.
func splitPatchTokens(text string) []string {
	tokens := []string{}
	for start := 0; start < len(text); {
		if end := strings.IndexByte(text[start:], '\n'); end >= 0 {
			tokens = append(tokens, text[start:start+end+1])
			start += end + 1
			continue
		}
		tokens = append(tokens, text[start:])
		start = len(text)
	}
	// Empty content has no tokens (jsdiff's tokenizer drops the lone empty
	// split element), so a pure insertion or deletion has no opposite lines.
	return tokens
}

// patchOpsFromComponents expands jsdiff components into per-token ops in diff
// order.
func patchOpsFromComponents(components []jsdiff.Component) []diffOp {
	var ops []diffOp
	oldIndex, newIndex := 0, 0
	for _, component := range components {
		for count := 0; count < component.Count; count++ {
			switch {
			case component.Added:
				ops = append(ops, diffOp{kind: opAdd, newIndex: newIndex})
				newIndex++
			case component.Removed:
				ops = append(ops, diffOp{kind: opRemove, oldIndex: oldIndex})
				oldIndex++
			default:
				ops = append(ops, diffOp{kind: opEqual, oldIndex: oldIndex, newIndex: newIndex})
				oldIndex++
				newIndex++
			}
		}
	}
	return ops
}

// patchHunk is one structuredPatch hunk before formatting.
type patchHunk struct {
	oldStart, oldLines int
	newStart, newLines int
	lines              []string
}

// patchPart is one run of the line diff (upstream diffLines' parts).
type patchPart struct {
	equal bool
	added bool
	lines []string
}

// buildPatchHunks groups diff ops into jsdiff structuredPatch hunks.
func buildPatchHunks(oldTokens, newTokens []string, ops []diffOp, context int) []patchHunk {
	parts := make([]patchPart, 0, len(ops)+1)
	for _, op := range ops {
		var part patchPart
		var line string
		switch op.kind {
		case opEqual:
			part.equal = true
			line = oldTokens[op.oldIndex]
		case opRemove:
			line = oldTokens[op.oldIndex]
		case opAdd:
			part.added = true
			line = newTokens[op.newIndex]
		}
		last := len(parts) - 1
		if last >= 0 && parts[last].equal == part.equal && parts[last].added == part.added {
			parts[last].lines = append(parts[last].lines, line)
			continue
		}
		parts = append(parts, patchPart{equal: part.equal, added: part.added, lines: []string{line}})
	}
	// The sentinel part makes the final context flush without special cases.
	parts = append(parts, patchPart{equal: true})

	contextLines := func(lines []string) []string {
		out := make([]string, len(lines))
		for i, line := range lines {
			out[i] = " " + line
		}
		return out
	}

	var hunks []patchHunk
	oldRangeStart, newRangeStart := 0, 0
	var current []string
	oldLine, newLine := 1, 1
	for index := 0; index < len(parts); index++ {
		part := parts[index]
		if !part.equal {
			if oldRangeStart == 0 {
				oldRangeStart, newRangeStart = oldLine, newLine
				if index > 0 && context > 0 {
					previous := parts[index-1].lines
					start := max(0, len(previous)-context)
					current = contextLines(previous[start:])
					oldRangeStart -= len(current)
					newRangeStart -= len(current)
				}
			}
			prefix := "-"
			if part.added {
				prefix = "+"
			}
			for _, line := range part.lines {
				current = append(current, prefix+line)
			}
			if part.added {
				newLine += len(part.lines)
			} else {
				oldLine += len(part.lines)
			}
			continue
		}
		if oldRangeStart != 0 {
			// An unchanged run of at most 2*context lines joins the hunks only
			// when another change follows it. Like jsdiff's
			// `i < diff.length - 2`, the last real run (just before the
			// sentinel at len(parts)-1) closes the range with up to context
			// lines rather than running through EOF.
			if len(part.lines) <= context*2 && index < len(parts)-2 {
				current = append(current, contextLines(part.lines)...)
			} else {
				contextSize := min(len(part.lines), context)
				current = append(current, contextLines(part.lines[:contextSize])...)
				hunks = append(hunks, patchHunk{
					oldStart: oldRangeStart, oldLines: oldLine - oldRangeStart + contextSize,
					newStart: newRangeStart, newLines: newLine - newRangeStart + contextSize,
					lines: current,
				})
				oldRangeStart, newRangeStart, current = 0, 0, nil
			}
		}
		oldLine += len(part.lines)
		newLine += len(part.lines)
	}
	return hunks
}

type opKind int

const (
	opEqual opKind = iota
	opRemove
	opAdd
)

type diffOp struct {
	kind     opKind
	oldIndex int
	newIndex int
}

// displayPart is one run of the diff by kind, split into lines the way
// upstream renders `part.value.split("\n")`.
type displayPart struct {
	kind  opKind
	lines []string
}

// displayParts diffs two texts and groups the components into line runs.
func displayParts(oldContent, newContent string) []displayPart {
	// diffLines() diffs newline-carrying tokens, so a line without its trailing
	// newline differs from one with it. Stripping the newline first would hide a
	// final-newline-only edit.
	oldTokens := splitPatchTokens(oldContent)
	newTokens := splitPatchTokens(newContent)
	components := jsdiff.DiffComponents(oldTokens, newTokens, func(a, b string) bool { return a == b })

	var parts []displayPart
	appendRun := func(kind opKind, tokens []string) {
		value := strings.Join(tokens, "")
		lines := strings.Split(value, "\n")
		if lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		if len(lines) == 0 {
			return
		}
		if len(parts) > 0 && parts[len(parts)-1].kind == kind {
			parts[len(parts)-1].lines = append(parts[len(parts)-1].lines, lines...)
			return
		}
		parts = append(parts, displayPart{kind: kind, lines: lines})
	}
	oldIndex, newIndex := 0, 0
	for _, component := range components {
		switch {
		case component.Added:
			appendRun(opAdd, newTokens[newIndex:newIndex+component.Count])
			newIndex += component.Count
		case component.Removed:
			appendRun(opRemove, oldTokens[oldIndex:oldIndex+component.Count])
			oldIndex += component.Count
		default:
			appendRun(opEqual, oldTokens[oldIndex:oldIndex+component.Count])
			oldIndex += component.Count
			newIndex += component.Count
		}
	}
	return parts
}

// displayLineNumWidth sizes the line-number column from `text.split("\n")`,
// which keeps the empty element after a trailing newline.
func displayLineNumWidth(text string) int {
	return len(fmt.Sprintf("%d", strings.Count(text, "\n")+1))
}

// GenerateDiffString builds the display-oriented diff with line numbers and
// context collapsing (port of generateDiffString).
func GenerateDiffString(oldContent, newContent string, contextLines int) (diff string, firstChangedLine int, ok bool) {
	if contextLines < 0 {
		// jsdiff has no default for a negative context; the port keeps the
		// display default rather than reproducing its slice quirks. Zero is
		// meaningful: no context lines, only the skipped-run markers.
		contextLines = 4
	}
	lineNumWidth := max(displayLineNumWidth(oldContent), displayLineNumWidth(newContent))
	lines, firstChanged, ok := formatDisplayDiff(displayParts(oldContent, newContent), 1, 1, lineNumWidth, contextLines)
	return strings.Join(lines, "\n"), firstChanged, ok
}

// formatDisplayDiff renders line parts with numbers starting at the given 1-based
// old and new lines, dropping context further than contextLines from a change
// (port of upstream's formatDisplayDiff).
func formatDisplayDiff(parts []displayPart, oldStartLine, newStartLine, lineNumWidth, contextLines int) (lines []string, firstChangedLine int, ok bool) {
	pad := func(n int) string {
		s := fmt.Sprintf("%d", n)
		for len(s) < lineNumWidth {
			s = " " + s
		}
		return s
	}

	output := lines[:0]
	oldLineNum, newLineNum := oldStartLine, newStartLine
	lastWasChange := false
	firstChanged := -1

	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if part.kind != opEqual {
			if firstChanged == -1 {
				firstChanged = newLineNum
			}
			for _, line := range part.lines {
				if part.kind == opAdd {
					output = append(output, "+"+pad(newLineNum)+" "+line)
					newLineNum++
				} else {
					output = append(output, "-"+pad(oldLineNum)+" "+line)
					oldLineNum++
				}
			}
			lastWasChange = true
			continue
		}

		raw := part.lines
		nextPartIsChange := i < len(parts)-1 && parts[i+1].kind != opEqual
		hasLeadingChange := lastWasChange
		hasTrailingChange := nextPartIsChange

		switch {
		case hasLeadingChange && hasTrailingChange:
			if len(raw) <= contextLines*2 {
				for _, line := range raw {
					output = append(output, " "+pad(oldLineNum)+" "+line)
					oldLineNum++
					newLineNum++
				}
			} else {
				leading := raw[:contextLines]
				trailing := raw[len(raw)-contextLines:]
				skipped := len(raw) - len(leading) - len(trailing)
				for _, line := range leading {
					output = append(output, " "+pad(oldLineNum)+" "+line)
					oldLineNum++
					newLineNum++
				}
				output = append(output, " "+strings.Repeat(" ", lineNumWidth)+" ...")
				oldLineNum += skipped
				newLineNum += skipped
				for _, line := range trailing {
					output = append(output, " "+pad(oldLineNum)+" "+line)
					oldLineNum++
					newLineNum++
				}
			}
		case hasLeadingChange:
			shown := raw
			if len(shown) > contextLines {
				shown = shown[:contextLines]
			}
			for _, line := range shown {
				output = append(output, " "+pad(oldLineNum)+" "+line)
				oldLineNum++
				newLineNum++
			}
			skipped := len(raw) - len(shown)
			if skipped > 0 {
				output = append(output, " "+strings.Repeat(" ", lineNumWidth)+" ...")
				oldLineNum += skipped
				newLineNum += skipped
			}
		case hasTrailingChange:
			skipped := max(0, len(raw)-contextLines)
			if skipped > 0 {
				output = append(output, " "+strings.Repeat(" ", lineNumWidth)+" ...")
				oldLineNum += skipped
				newLineNum += skipped
			}
			for _, line := range raw[skipped:] {
				output = append(output, " "+pad(oldLineNum)+" "+line)
				oldLineNum++
				newLineNum++
			}
		default:
			oldLineNum += len(raw)
			newLineNum += len(raw)
		}
		lastWasChange = false
	}

	// ok reports whether anything changed (upstream returns an undefined
	// firstChangedLine for identical content).
	return output, firstChanged, firstChanged >= 0
}

// diffWindowMarginLines is how many unchanged lines each side of a replacement
// a diff window keeps: well above the 4 context lines shown, so jsdiff aligns a
// change within its window as it would over the whole file (upstream
// DIFF_WINDOW_MARGIN_LINES).
const diffWindowMarginLines = 32

// EditDiff is the edit tool's display diff and unified patch.
type EditDiff struct {
	Diff  string
	Patch string
	// FirstChangedLine is 1-based in the new file; -1 when nothing changed.
	FirstChangedLine int
}

// diffWindow is a matching range of the old and new contents around one or more
// replacements: byte offsets and 0-based first lines.
type diffWindow struct {
	oldStart, oldEnd int
	oldLine          int
	newStart, newEnd int
	newLine          int
}

// lineStartBefore is the start of the line that contains offset; offset 0 is the
// start of the text (JS lastIndexOf treats a negative position as 0).
func lineStartBefore(text string, offset int) int {
	if offset <= 0 {
		return 0
	}
	return strings.LastIndex(text[:offset], "\n") + 1
}

// countLineFeeds counts the "\n" before end, starting the search at start.
func countLineFeeds(text string, start, end int) int {
	if start < 0 {
		start = 0
	}
	if end > len(text) {
		end = len(text)
	}
	if start >= end {
		return 0
	}
	return strings.Count(text[start:end], "\n")
}

// getDiffWindows groups the replacements (sorted and disjoint) into whole-line
// windows with a margin, merging windows that overlap (upstream getDiffWindows).
func getDiffWindows(baseContent string, replacements []textReplacement) []diffWindow {
	var windows []diffWindow
	offsetDelta, lineDelta := 0, 0
	cursorOffset, cursorLine := 0, 0
	for _, replacement := range replacements {
		matchEnd := replacement.matchIndex + replacement.matchLength
		start := lineStartBefore(baseContent, replacement.matchIndex)
		for margin := 0; margin < diffWindowMarginLines && start > 0; margin++ {
			start = lineStartBefore(baseContent, start-1)
		}
		end := matchEnd
		for margin := 0; margin <= diffWindowMarginLines && end < len(baseContent); margin++ {
			from := end
			if margin == 0 {
				from = max(matchEnd-1, replacement.matchIndex)
			}
			lineFeed := -1
			if from < len(baseContent) {
				lineFeed = strings.IndexByte(baseContent[from:], '\n')
				if lineFeed != -1 {
					lineFeed += from
				}
			}
			if lineFeed == -1 {
				end = len(baseContent)
			} else {
				end = lineFeed + 1
			}
		}
		replacementOffsetDelta := len(replacement.newText) - replacement.matchLength
		replacementLineDelta := countLineFeeds(replacement.newText, 0, len(replacement.newText)) -
			countLineFeeds(baseContent, replacement.matchIndex, matchEnd)
		if current := len(windows) - 1; current >= 0 && start <= windows[current].oldEnd {
			windows[current].oldEnd = max(windows[current].oldEnd, end)
			windows[current].newEnd = windows[current].oldEnd + offsetDelta + replacementOffsetDelta
		} else {
			cursorLine += countLineFeeds(baseContent, cursorOffset, start)
			cursorOffset = start
			windows = append(windows, diffWindow{
				oldStart: start, oldEnd: end, oldLine: cursorLine,
				newStart: start + offsetDelta, newEnd: end + offsetDelta + replacementOffsetDelta,
				newLine: cursorLine + lineDelta,
			})
		}
		offsetDelta += replacementOffsetDelta
		lineDelta += replacementLineDelta
	}
	return windows
}

// GenerateEditDiff diffs an edit only around its replacements, so the cost
// follows the size of the change instead of the file (pi v1.5.2
// core/tools/edit-diff.ts generateEditDiff). The result matches
// GenerateDiffString and GenerateUnifiedPatch over the whole contents, except
// that jsdiff may align runs of repeated lines differently within a window.
func GenerateEditDiff(path, baseContent, newContent string, replacements []textReplacement, contextLines int) EditDiff {
	if contextLines < 0 {
		contextLines = 4
	}
	lineNumWidth := max(displayLineNumWidth(baseContent), displayLineNumWidth(newContent))
	skipMarker := " " + strings.Repeat(" ", lineNumWidth) + " ..."
	var output []string
	var hunks []patchHunk
	firstChanged := -1
	for _, window := range getDiffWindows(baseContent, replacements) {
		oldText := baseContent[window.oldStart:window.oldEnd]
		newText := newContent[window.newStart:window.newEnd]
		lines, first, ok := formatDisplayDiff(displayParts(oldText, newText), window.oldLine+1, window.newLine+1, lineNumWidth, contextLines)
		if firstChanged == -1 && ok {
			firstChanged = first
		}
		// Both windows around a run of unchanged lines mark it as skipped; the
		// whole-file diff shows one marker.
		if len(output) > 0 && len(lines) > 0 && output[len(output)-1] == skipMarker && lines[0] == skipMarker {
			lines = lines[1:]
		}
		output = append(output, lines...)

		oldTokens := splitPatchTokens(oldText)
		newTokens := splitPatchTokens(newText)
		ops := patchOpsFromComponents(jsdiff.DiffComponents(oldTokens, newTokens, func(a, b string) bool { return a == b }))
		for _, hunk := range buildPatchHunks(oldTokens, newTokens, ops, contextLines) {
			hunk.oldStart += window.oldLine
			hunk.newStart += window.newLine
			hunks = append(hunks, hunk)
		}
	}
	return EditDiff{
		Diff:             strings.Join(output, "\n"),
		Patch:            formatPatchHunks(path, hunks),
		FirstChangedLine: firstChanged,
	}
}
