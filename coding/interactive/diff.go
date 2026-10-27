package interactive

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/dat267/pier/internal/jsdiff"
	"github.com/dat267/pier/tui"
)

// Port of src/modes/interactive/components/diff.ts plus the jsdiff
// (`diff` package, v8) word-diff it depends on: the tokenizer, the Myers diff
// with the diagonal pruning optimisation, the word-diff post-processing that
// rebalances whitespace between change objects, and the renderDiff line
// formatter with intra-line highlighting.
//
// The npm package is unavailable offline, so the algorithm is ported directly
// from its source (divergence D87) and verified against the real library
// through Node goldens.

const extendedWordChars = `a-zA-Z0-9_\x{00AD}\x{00C0}-\x{00D6}\x{00D8}-\x{00F6}\x{00F8}-\x{02C6}\x{02C8}-\x{02D7}\x{02DE}-\x{02FF}\x{1E00}-\x{1EFF}`

// jsdiff uses the JS `\s` class (Unicode whitespace); Go's `\s` is ASCII-only.
const jsWhitespaceChars = `\t\n\v\f\r \x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}`

var wordTokenizeRegex = regexp.MustCompile(`[` + extendedWordChars + `]+|[` + jsWhitespaceChars + `]+|[^` + extendedWordChars + `]`)
var whitespaceRegex = regexp.MustCompile(`[` + jsWhitespaceChars + `]`)

// DiffChange is a diff change object.
type DiffChange struct {
	Value   string `json:"value"`
	Added   bool   `json:"added,omitempty"`
	Removed bool   `json:"removed,omitempty"`
	Count   int    `json:"count,omitempty"`
}

type diffComponent struct {
	count   int
	added   bool
	removed bool
}

// DiffWords computes a word-level diff between two strings, mirroring
// jsdiff's diffWords (whitespace attached to adjacent tokens).
func DiffWords(oldStr string, newStr string) []DiffChange {
	oldTokens := diffWordTokenize(oldStr)
	newTokens := diffWordTokenize(newStr)
	return diffTokens(oldTokens, newTokens)
}

// diffWordTokenize mirrors WordDiff.tokenize: split into word/whitespace runs
// and merge whitespace into the neighbouring token.
func diffWordTokenize(value string) []string {
	parts := wordTokenizeRegex.FindAllString(value, -1)
	var tokens []string
	prevPart := ""
	hasPrev := false
	for _, part := range parts {
		switch {
		case whitespaceRegex.MatchString(part):
			if !hasPrev {
				tokens = append(tokens, part)
			} else {
				last := tokens[len(tokens)-1]
				tokens = tokens[:len(tokens)-1]
				tokens = append(tokens, last+part)
			}
		case hasPrev && whitespaceRegex.MatchString(prevPart):
			if tokens[len(tokens)-1] == prevPart {
				last := tokens[len(tokens)-1]
				tokens = tokens[:len(tokens)-1]
				tokens = append(tokens, last+part)
			} else {
				tokens = append(tokens, prevPart+part)
			}
		default:
			tokens = append(tokens, part)
		}
		prevPart = part
		hasPrev = true
	}
	return tokens
}

// diffJoin mirrors WordDiff.join: strip the leading whitespace of every token
// after the first, then concatenate.
func diffJoin(tokens []string) string {
	var builder strings.Builder
	for index, token := range tokens {
		if index == 0 {
			builder.WriteString(token)
			continue
		}
		// Upstream strips the JS `\s` class (Unicode whitespace).
		builder.WriteString(strings.TrimLeftFunc(token, isSpaceRune))
	}
	return builder.String()
}

func diffTokens(oldTokens []string, newTokens []string) []DiffChange {
	// removeEmpty: drop falsy tokens.
	filtered := func(tokens []string) []string {
		var out []string
		for _, token := range tokens {
			if token != "" {
				out = append(out, token)
			}
		}
		return out
	}
	oldTokens = filtered(oldTokens)
	newTokens = filtered(newTokens)

	components := diffComponents(oldTokens, newTokens)
	changes := buildDiffValues(components, newTokens, oldTokens)
	return postProcessWordDiff(changes)
}

func wordEquals(left string, right string) bool {
	return strings.TrimSpace(left) == strings.TrimSpace(right)
}

// diffComponents returns the jsdiff components for the word tokens, comparing
// with the word-diff whitespace rule.
func diffComponents(oldTokens []string, newTokens []string) []*diffComponent {
	components := jsdiff.DiffComponents(oldTokens, newTokens, wordEquals)
	out := make([]*diffComponent, len(components))
	for index, component := range components {
		out[index] = &diffComponent{count: component.Count, added: component.Added, removed: component.Removed}
	}
	return out
}

func buildDiffValues(components []*diffComponent, newTokens []string, oldTokens []string) []DiffChange {
	changes := make([]DiffChange, 0, len(components))
	newPos := 0
	oldPos := 0
	for _, component := range components {
		if !component.removed {
			end := min(newPos+component.count, len(newTokens))
			change := DiffChange{Value: diffJoin(newTokens[newPos:end])}
			if !component.added {
				change.Added = false
				newPos = end
				oldPos += component.count
			} else {
				change.Added = true
				newPos = end
			}
			changes = append(changes, change)
			continue
		}
		end := min(oldPos+component.count, len(oldTokens))
		changes = append(changes, DiffChange{Value: diffJoin(oldTokens[oldPos:end]), Removed: true})
		oldPos = end
	}
	return changes
}

// ---- Word-diff whitespace post-processing ----

func postProcessWordDiff(changes []DiffChange) []DiffChange {
	var lastKeep *DiffChange
	insertion := -1
	deletion := -1
	for index := range changes {
		change := &changes[index]
		switch {
		case change.Added:
			insertion = index
		case change.Removed:
			deletion = index
		default:
			if insertion != -1 || deletion != -1 {
				dedupeWhitespaceInChangeObjects(lastKeep, deletion, insertion, change, changes)
			}
			lastKeep = change
			insertion = -1
			deletion = -1
		}
	}
	if insertion != -1 || deletion != -1 {
		dedupeWhitespaceInChangeObjects(lastKeep, deletion, insertion, nil, changes)
	}
	return changes
}

func dedupeWhitespaceInChangeObjects(startKeep *DiffChange, deletionIndex int, insertionIndex int, endKeep *DiffChange, changes []DiffChange) {
	var deletion, insertion *DiffChange
	if deletionIndex >= 0 {
		deletion = &changes[deletionIndex]
	}
	if insertionIndex >= 0 {
		insertion = &changes[insertionIndex]
	}

	if deletion != nil && insertion != nil {
		oldWsPrefix, oldWsSuffix := leadingAndTrailingWs(deletion.Value)
		newWsPrefix, newWsSuffix := leadingAndTrailingWs(insertion.Value)
		if startKeep != nil {
			commonWsPrefix := longestCommonPrefix(oldWsPrefix, newWsPrefix)
			startKeep.Value = replaceSuffix(startKeep.Value, newWsPrefix, commonWsPrefix)
			deletion.Value = removePrefix(deletion.Value, commonWsPrefix)
			insertion.Value = removePrefix(insertion.Value, commonWsPrefix)
		}
		if endKeep != nil {
			commonWsSuffix := longestCommonSuffix(oldWsSuffix, newWsSuffix)
			endKeep.Value = replacePrefix(endKeep.Value, newWsSuffix, commonWsSuffix)
			deletion.Value = removeSuffix(deletion.Value, commonWsSuffix)
			insertion.Value = removeSuffix(insertion.Value, commonWsSuffix)
		}
		return
	}

	if insertion != nil {
		// Insertions keep their trailing whitespace; duplicate leading
		// whitespace is removed from the following keep instead.
		if startKeep != nil {
			ws := leadingWs(insertion.Value)
			insertion.Value = insertion.Value[len(ws):]
		}
		if endKeep != nil {
			ws := leadingWs(endKeep.Value)
			endKeep.Value = endKeep.Value[len(ws):]
		}
		return
	}

	if startKeep != nil && endKeep != nil {
		newWsFull := leadingWs(endKeep.Value)
		delWsStart, delWsEnd := leadingAndTrailingWs(deletion.Value)
		newWsStart := longestCommonPrefix(newWsFull, delWsStart)
		deletion.Value = removePrefix(deletion.Value, newWsStart)
		newWsEnd := longestCommonSuffix(removePrefix(newWsFull, newWsStart), delWsEnd)
		deletion.Value = removeSuffix(deletion.Value, newWsEnd)
		endKeep.Value = replacePrefix(endKeep.Value, newWsFull, newWsEnd)
		startKeep.Value = replaceSuffix(startKeep.Value, newWsFull, newWsFull[:len(newWsFull)-len(newWsEnd)])
		return
	}

	if endKeep != nil {
		endKeepWsPrefix := leadingWs(endKeep.Value)
		deletionWsSuffix := trailingWs(deletion.Value)
		overlap := maximumOverlap(deletionWsSuffix, endKeepWsPrefix)
		deletion.Value = removeSuffix(deletion.Value, overlap)
		return
	}

	if startKeep != nil {
		startKeepWsSuffix := trailingWs(startKeep.Value)
		deletionWsPrefix := leadingWs(deletion.Value)
		overlap := maximumOverlap(startKeepWsSuffix, deletionWsPrefix)
		deletion.Value = removePrefix(deletion.Value, overlap)
	}
}

func leadingAndTrailingWs(value string) (string, string) {
	return leadingWs(value), trailingWs(value)
}

func leadingWs(value string) string {
	index := 0
	for index < len(value) {
		r, size := decodeRuneAt(value, index)
		if !tui.IsWhitespaceChar(string(r)) {
			break
		}
		index += size
	}
	return value[:index]
}

func trailingWs(value string) string {
	end := len(value)
	for end > 0 {
		r, size := decodeLastRuneAt(value, end)
		if !tui.IsWhitespaceChar(string(r)) {
			break
		}
		end -= size
	}
	return value[end:]
}

func longestCommonPrefix(a string, b string) string {
	index := 0
	for index < len(a) && index < len(b) && a[index] == b[index] {
		index++
	}
	return a[:index]
}

func longestCommonSuffix(a string, b string) string {
	if a == "" || b == "" || a[len(a)-1] != b[len(b)-1] {
		return ""
	}
	count := 0
	for count < len(a) && count < len(b) && a[len(a)-(count+1)] == b[len(b)-(count+1)] {
		count++
	}
	return a[len(a)-count:]
}

func replacePrefix(value string, oldPrefix string, newPrefix string) string {
	if len(value) < len(oldPrefix) || value[:len(oldPrefix)] != oldPrefix {
		panic("string does not start with prefix")
	}
	return newPrefix + value[len(oldPrefix):]
}

func replaceSuffix(value string, oldSuffix string, newSuffix string) string {
	if oldSuffix == "" {
		return value + newSuffix
	}
	if len(value) < len(oldSuffix) || value[len(value)-len(oldSuffix):] != oldSuffix {
		panic("string does not end with suffix")
	}
	return value[:len(value)-len(oldSuffix)] + newSuffix
}

func removePrefix(value string, prefix string) string { return replacePrefix(value, prefix, "") }
func removeSuffix(value string, suffix string) string { return replaceSuffix(value, suffix, "") }

func maximumOverlap(a string, b string) string {
	return b[:overlapCount(a, b)]
}

func overlapCount(a string, b string) int {
	startA := 0
	if len(a) > len(b) {
		startA = len(a) - len(b)
	}
	endB := min(len(a), len(b))
	mapping := make([]int, max(1, endB))
	k := 0
	if endB > 0 {
		mapping[0] = 0
	}
	for j := 1; j < endB; j++ {
		if b[j] == b[k] {
			mapping[j] = mapping[k]
		} else {
			mapping[j] = k
		}
		for k > 0 && b[j] != b[k] {
			k = mapping[k]
		}
		if b[j] == b[k] {
			k++
		}
	}
	k = 0
	for i := startA; i < len(a); i++ {
		for k > 0 && (k >= len(b) || a[i] != b[k]) {
			k = mapping[k]
		}
		if k < len(b) && a[i] == b[k] {
			k++
		}
	}
	return k
}

// ---- renderDiff ----

var diffLineRegex = regexp.MustCompile(`^([-+\t\n\f\r ])([\t\n\f\r ]*\d*)[\t\n\f\r ](.*)$`)

type parsedDiffLine struct {
	prefix  string
	lineNum string
	content string
}

func parseDiffLine(line string) (parsedDiffLine, bool) {
	match := diffLineRegex.FindStringSubmatch(line)
	if match == nil {
		return parsedDiffLine{}, false
	}
	return parsedDiffLine{prefix: match[1], lineNum: match[2], content: match[3]}, true
}

func replaceTabs(text string) string { return strings.ReplaceAll(text, "\t", "   ") }

// RenderDiffOptions configure RenderDiff.
type RenderDiffOptions struct {
	FilePath string
}

// RenderIntraLineDiff computes the word-level diff and applies inverse styling
// to the changed parts (leading whitespace of the first changed part is not
// highlighted).
func RenderIntraLineDiff(oldContent string, newContent string) (string, string) {
	wordDiff := DiffWords(oldContent, newContent)
	theme := ActiveTheme()

	var removedLine strings.Builder
	var addedLine strings.Builder
	isFirstRemoved := true
	isFirstAdded := true

	for _, part := range wordDiff {
		switch {
		case part.Removed:
			value := part.Value
			if isFirstRemoved {
				leading := leadingWs(value)
				value = value[len(leading):]
				removedLine.WriteString(leading)
				isFirstRemoved = false
			}
			if value != "" {
				removedLine.WriteString(theme.Inverse(value))
			}
		case part.Added:
			value := part.Value
			if isFirstAdded {
				leading := leadingWs(value)
				value = value[len(leading):]
				addedLine.WriteString(leading)
				isFirstAdded = false
			}
			if value != "" {
				addedLine.WriteString(theme.Inverse(value))
			}
		default:
			removedLine.WriteString(part.Value)
			addedLine.WriteString(part.Value)
		}
	}
	return removedLine.String(), addedLine.String()
}

// RenderDiff renders a diff string with colored lines and intra-line change
// highlighting.
func RenderDiff(diffText string, options RenderDiffOptions) string {
	theme := ActiveTheme()
	lines := strings.Split(diffText, "\n")
	var result []string

	index := 0
	for index < len(lines) {
		line := lines[index]
		parsed, ok := parseDiffLine(line)
		if !ok {
			result = append(result, theme.Fg("toolDiffContext", line))
			index++
			continue
		}

		switch parsed.prefix {
		case "-":
			type numberedLine struct{ lineNum, content string }
			var removedLines []numberedLine
			for index < len(lines) {
				p, ok := parseDiffLine(lines[index])
				if !ok || p.prefix != "-" {
					break
				}
				removedLines = append(removedLines, numberedLine{p.lineNum, p.content})
				index++
			}
			var addedLines []numberedLine
			for index < len(lines) {
				p, ok := parseDiffLine(lines[index])
				if !ok || p.prefix != "+" {
					break
				}
				addedLines = append(addedLines, numberedLine{p.lineNum, p.content})
				index++
			}

			if len(removedLines) == 1 && len(addedLines) == 1 {
				removed := removedLines[0]
				added := addedLines[0]
				removedLine, addedLine := RenderIntraLineDiff(replaceTabs(removed.content), replaceTabs(added.content))
				result = append(result, theme.Fg("toolDiffRemoved", "-"+removed.lineNum+" "+removedLine))
				result = append(result, theme.Fg("toolDiffAdded", "+"+added.lineNum+" "+addedLine))
				continue
			}
			for _, removed := range removedLines {
				result = append(result, theme.Fg("toolDiffRemoved", "-"+removed.lineNum+" "+replaceTabs(removed.content)))
			}
			for _, added := range addedLines {
				result = append(result, theme.Fg("toolDiffAdded", "+"+added.lineNum+" "+replaceTabs(added.content)))
			}
		case "+":
			result = append(result, theme.Fg("toolDiffAdded", "+"+parsed.lineNum+" "+replaceTabs(parsed.content)))
			index++
		default:
			result = append(result, theme.Fg("toolDiffContext", " "+parsed.lineNum+" "+replaceTabs(parsed.content)))
			index++
		}
	}
	return strings.Join(result, "\n")
}

func decodeRuneAt(value string, index int) (rune, int) {
	r, size := utf8.DecodeRuneInString(value[index:])
	return r, size
}

func decodeLastRuneAt(value string, end int) (rune, int) {
	r, size := utf8.DecodeLastRuneInString(value[:end])
	return r, size
}
