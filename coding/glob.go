package coding

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/dat267/pier/ai"
)

// MatchGlob matches a value against a minimatch-style pattern as used by
// model-scope resolution (upstream uses minimatch with { nocase: true }).
//
// Supported syntax: `*` (any run of characters except "/"), a whole-segment `**`
// (any run of path segments, never crossing into a segment that starts with
// "."), `?` (one character except "/"), `[...]` character classes with ranges
// and `!`/`^` negation, brace groups (comma alternatives and `{1..3}`-style
// sequences) and `\` escaping.
//
// D22: minimatch's extended globs (`!(...)`, `+(...)`, `?(...)`, `@(...)`,
// `*(...)`), extglob negation patterns (`!pattern`), its POSIX character
// classes, `\`-escaped separators, and its `matchBase`/`nocase` options beyond
// ASCII case folding are not implemented; model ids never use them. D262 covers
// the one remaining difference, in minimatch's escaped-suffix fast path.
func MatchGlob(pattern, value string, nocase bool) bool {
	// An empty pattern matches only the empty path (minimatch does not treat "/"
	// as an empty match).
	if pattern == "" {
		return value == ""
	}
	valueSegments := globSegments(value)
	if value == "" {
		// An empty path has one empty segment, which a globstar may consume.
		valueSegments = []string{""}
	}
	for _, expanded := range expandBraces(pattern) {
		if matchSegmentList(canonicalGlobPattern(expanded), valueSegments, nocase) {
			return true
		}
	}
	return false
}

const (
	// globBraceExpansionMax and globBraceExpansionMaxLength bound brace
	// expansion like brace-expansion's EXPANSION_MAX and EXPANSION_MAX_LENGTH
	// (the CVE-2026-14257 fix): a pattern with many brace groups is truncated
	// instead of exhausting memory.
	globBraceExpansionMax         = 100_000
	globBraceExpansionMaxLength   = 4_000_000
	globBraceExpansionMaxDepth    = 1_000
	globBraceExpansionMaxRewrites = 1_000
)

// expandBraces expands brace groups like brace-expansion: a comma group and a
// numeric or alphabetic sequence expand, a group with neither stays literal, and
// brace groups nested inside a non-expanding group still expand inner-first
// ("{a{b,c}}" is "{ab}" and "{ac}"). A backslash escapes the character after it.
// The expansion is bounded by the limits below.
func expandBraces(pattern string) []string {
	// minimatch only invokes brace-expansion when this syntax is present. That
	// layer unescapes backslash pairs in addition to the escaped punctuation.
	unescapeSlashes := globBraceExpansionRegex.MatchString(pattern)
	// brace-expansion preserves a leading empty group even before a comma set.
	if strings.HasPrefix(pattern, "{}") {
		pattern = `\{\}` + pattern[2:]
	}
	return unescapeBracePatterns(expandBracesWithin(pattern, true, 0), unescapeSlashes)
}

// expandBracesWithin mirrors brace-expansion's expand_: it consumes the
// pattern's brace groups left to right while combining a running accumulator.
// top marks the outermost call, where an empty alternative is dropped (Bash
// behavior) for a comma group.
func expandBracesWithin(pattern string, top bool, depth int) []string {
	if depth > globBraceExpansionMaxDepth {
		return []string{pattern}
	}
	acc := []string{""}
	dropEmpties := false
	firstGroup := true
	rewrites := 0
	current := pattern
	for {
		open, close := globBraceGroup(current)
		if open < 0 {
			return combineExpansions(acc, current, []string{""}, dropEmpties)
		}
		pre, body, post := current[:open], current[open+1:close], current[close+1:]
		if strings.HasSuffix(pre, "$") {
			acc = combineExpansions(acc, current[:close+1], []string{""}, dropEmpties && post == "")
			firstGroup = false
			if post == "" {
				return acc
			}
			current = post
			continue
		}
		isAlphaSequence, isSequence := braceSequenceKind(body)
		if !isSequence && !strings.Contains(body, ",") {
			if rewrites < globBraceExpansionMaxRewrites && globBraceRewriteRegex.MatchString(post) {
				rewrites++
				current = pre + "{" + body + `\}` + post
				top = true
				continue
			}
			// Nothing here expands; the rest of the string is literal.
			return combineExpansions(acc, current, []string{""}, dropEmpties)
		}
		if firstGroup {
			dropEmpties = top && !isSequence
			firstGroup = false
		}
		var values []string
		if isSequence {
			values = expandBraceSequence(body, isAlphaSequence)
		} else {
			parts := splitTopLevel(body)
			if len(parts) == 1 {
				// A comma nested in a brace inside the body: expand the body and
				// keep its braces ("{a{b,c}}" -> "{ab}", "{ac}").
				for _, expanded := range expandBracesWithin(parts[0], false, depth+1) {
					values = append(values, "{"+expanded+"}")
				}
			} else {
				dropEmptyValues := dropEmpties && post == "" && pre == ""
				for _, prefix := range acc {
					if prefix != "" {
						dropEmptyValues = false
						break
					}
				}
				length := 0
			alternatives:
				for _, part := range parts {
					expandedParts := expandBracesWithin(part, false, depth+1)
					if values == nil {
						values = make([]string, 0, globExpansionCapacity(len(parts), len(expandedParts)))
					}
					for _, expanded := range expandedParts {
						if dropEmptyValues && expanded == "" {
							continue
						}
						if len(values) >= globBraceExpansionMax || length+len(expanded) > globBraceExpansionMaxLength {
							break alternatives
						}
						values = append(values, expanded)
						length += len(expanded)
					}
				}
			}
		}
		// Bash drops empty results only when nothing follows the group.
		acc = combineExpansions(acc, pre, values, dropEmpties && post == "")
		if len(post) == 0 {
			return acc
		}
		current = post
	}
}

// combineExpansions builds acc[a] + pre + values[v] for every combination,
// stopping at the expansion limits. Subsequent groups and literal suffixes must
// still be appended to the retained alternatives.
func combineExpansions(acc []string, pre string, values []string, dropEmpties bool) []string {
	out := make([]string, 0, globExpansionCapacity(len(acc), len(values)))
	length := 0
	for _, prefix := range acc {
		for _, value := range values {
			if len(out) >= globBraceExpansionMax {
				return out
			}
			expansion := prefix + pre + value
			if dropEmpties && expansion == "" {
				continue
			}
			if length+len(expansion) > globBraceExpansionMaxLength {
				return out
			}
			length += len(expansion)
			out = append(out, expansion)
		}
	}
	return out
}

func globExpansionCapacity(left, right int) int {
	if right == 0 {
		return 0
	}
	if left > globBraceExpansionMax/right {
		return globBraceExpansionMax
	}
	return left * right
}

// braceSequenceKind reports whether a brace body is a numeric or alphabetic
// sequence ("1..5", "a..e..2").
func braceSequenceKind(body string) (isAlpha bool, isSequence bool) {
	if globNumericSequenceRegex.MatchString(body) {
		return false, true
	}
	if globAlphaSequenceRegex.MatchString(body) {
		return true, true
	}
	return false, false
}

var (
	globNumericSequenceRegex = regexp.MustCompile(`^-?\d+\.\.-?\d+(?:\.\.-?\d+)?$`)
	globAlphaSequenceRegex   = regexp.MustCompile(`^[a-zA-Z]\.\.[a-zA-Z](?:\.\.-?\d+)?$`)
	globBraceRewriteRegex    = regexp.MustCompile(`,(?:[^,\r\n\x{2028}\x{2029}][^\r\n\x{2028}\x{2029}]*)?}`)
	globBraceExpansionRegex  = regexp.MustCompile(`\{[^{\r\n\x{2028}\x{2029}]*\}`)
)

// expandBraceSequence expands "1..5", "5..1", "01..05" and "a..e..2", stopping
// at the expansion limits.
func expandBraceSequence(body string, isAlpha bool) (values []string) {
	parts := strings.Split(body, "..")
	if len(parts) < 2 {
		return nil
	}
	from := braceNumericValue(parts[0])
	to := braceNumericValue(parts[1])
	width := max(len(parts[0]), len(parts[1]))
	step := float64(1)
	if len(parts) == 3 {
		step = math.Max(math.Abs(braceNumericValue(parts[2])), 1)
	}
	reverse := to < from
	if reverse {
		step = -step
	}
	padded := false
	for _, part := range parts {
		if globPaddedSequenceRegex.MatchString(part) {
			padded = true
			break
		}
	}
	total := 0
	for value := from; (!reverse && value <= to) || (reverse && value >= to); value += step {
		if len(values) >= globBraceExpansionMax {
			return values
		}
		var text string
		if isAlpha {
			text = string(rune(value))
			if text == "\\" {
				text = ""
			}
		} else if padded {
			text = padBraceNumber(value, width)
		} else {
			text = ai.JSNumberToString(value)
		}
		if total+len(text) > globBraceExpansionMaxLength {
			return values
		}
		total += len(text)
		values = append(values, text)
	}
	return values
}

var globPaddedSequenceRegex = regexp.MustCompile(`^-?0\d`)

// braceNumericValue is brace-expansion's numeric(): a numeric string is its
// value, anything else is the first character's code point.
func braceNumericValue(text string) float64 {
	if value, err := strconv.ParseFloat(text, 64); err == nil || math.IsInf(value, 0) {
		return value
	}
	if text == "" {
		return 0
	}
	return float64(text[0])
}

// padBraceNumber zero-pads a numeric sequence member to width.
func padBraceNumber(value float64, width int) string {
	digits := ai.JSNumberToString(value)
	negative := strings.HasPrefix(digits, "-")
	if negative {
		digits = digits[1:]
		width--
	}
	if len(digits) < width {
		digits = strings.Repeat("0", width-len(digits)) + digits
	}
	if negative {
		return "-" + digits
	}
	return digits
}

// unescapeBracePatterns turns the brace-expansion escapes into their literal
// characters; the glob matcher handles every other escape.
func unescapeBracePatterns(patterns []string, unescapeSlashes bool) []string {
	for index, pattern := range patterns {
		if strings.Contains(pattern, "\\") {
			patterns[index] = unescapeBracePattern(pattern, unescapeSlashes)
		}
	}
	return patterns
}

// unescapeBracePattern turns the brace-expansion escapes into their literal
// characters.
func unescapeBracePattern(pattern string, unescapeSlashes bool) string {
	var out strings.Builder
	out.Grow(len(pattern))
	for index := 0; index < len(pattern); index++ {
		if pattern[index] == '\\' && index+1 < len(pattern) {
			switch pattern[index+1] {
			case '{', '}', ',', '.':
				out.WriteByte(pattern[index+1])
				index++
				continue
			case '\\':
				out.WriteByte('\\')
				if !unescapeSlashes {
					out.WriteByte('\\')
				}
				index++
				continue
			}
		}
		out.WriteByte(pattern[index])
	}
	return out.String()
}

// globBraceGroup returns the first balanced unescaped brace group. Like
// balanced-match, it falls back to a complete inner group if an outer opening
// brace has no matching close (open is -1 when the pattern has no group).
func globBraceGroup(pattern string) (open, close int) {
	open, close = -1, -1
	var openings []int
	for index := 0; index < len(pattern); index++ {
		switch pattern[index] {
		case '\\':
			index++
		case '{':
			openings = append(openings, index)
		case '}':
			if len(openings) > 0 {
				start := openings[len(openings)-1]
				openings = openings[:len(openings)-1]
				if len(openings) == 0 {
					return start, index
				}
				if open < 0 || start < open {
					open, close = start, index
				}
			}
		}
	}
	return open, close
}

// splitTopLevel splits a brace body on its unescaped top-level commas.
func splitTopLevel(body string) []string {
	var parts []string
	depth := 0
	current := strings.Builder{}
	for index := 0; index < len(body); index++ {
		switch body[index] {
		case '\\':
			current.WriteByte(body[index])
			index++
			if index < len(body) {
				current.WriteByte(body[index])
			}
		case '{':
			depth++
			current.WriteByte(body[index])
		case '}':
			depth--
			current.WriteByte(body[index])
		case ',':
			if depth == 0 {
				parts = append(parts, current.String())
				current.Reset()
				continue
			}
			current.WriteByte(body[index])
		default:
			current.WriteByte(body[index])
		}
	}
	parts = append(parts, current.String())
	return parts
}

// canonicalGlobPattern applies minimatch's level-one optimization to an expanded
// pattern: adjacent globstars collapse and a ".." segment removes the segment
// before it unless that is "..", "." or "**". A pattern reduced to nothing is the
// empty segment, which matches "" and "/".
func canonicalGlobPattern(pattern string) []string {
	segments := globSegments(pattern)
	out := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment == "**" && len(out) > 0 && out[len(out)-1] == "**" {
			continue
		}
		if segment == ".." && len(out) > 0 {
			previous := out[len(out)-1]
			if previous != ".." && previous != "." && previous != "**" && previous != "" {
				out = out[:len(out)-1]
				continue
			}
		}
		out = append(out, segment)
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// globSegments splits a pattern or value into path segments the way minimatch
// does by default (preserveMultipleSlashes is off): runs of separators collapse,
// and a trailing separator stays as one empty segment so "a/" keeps requiring
// one while "b/" still matches the pattern "b".
func globSegments(path string) []string {
	if path == "" {
		return nil
	}
	segments := make([]string, 0, 4)
	for index, segment := range strings.Split(path, "/") {
		if segment != "" {
			segments = append(segments, segment)
			continue
		}
		// Keep the empty segment that marks a leading slash (an absolute path);
		// the other runs collapse.
		if index == 0 {
			segments = append(segments, "")
		}
	}
	if strings.HasSuffix(path, "/") {
		segments = append(segments, "")
	}
	return segments
}

// matchSegmentList matches the pattern's path segments against the value's,
// treating a whole-segment `**` as a globstar and every other segment as a
// single-segment glob.
func matchSegmentList(pattern, value []string, nocase bool) bool {
	// dotFreeFrom[index] reports that no segment from index on starts with a
	// dot, which a globstar will not enter.
	dotFreeFrom := make([]bool, len(value)+1)
	dotFreeFrom[len(value)] = true
	for index := len(value) - 1; index >= 0; index-- {
		dotFreeFrom[index] = dotFreeFrom[index+1] && !isDotSegment(value[index])
	}

	type state struct{ pattern, value int }
	memo := make(map[state]bool)
	seen := make(map[state]bool)
	var match func(patternIndex, valueIndex int) bool
	match = func(patternIndex, valueIndex int) bool {
		key := state{patternIndex, valueIndex}
		if seen[key] {
			return memo[key]
		}
		result := false
		switch {
		case patternIndex == len(pattern):
			// Extra trailing separators in the value are allowed (minimatch
			// preserves neither, but accepts "b/" for the pattern "b").
			result = true
			for _, segment := range value[valueIndex:] {
				if segment != "" {
					result = false
					break
				}
			}
		case pattern[patternIndex] == "**":
			if patternIndex == len(pattern)-1 {
				// A trailing globstar consumes every remaining segment, so it needs
				// at least one (minimatch: "a/**" does not match "a"). A globstar
				// that is the whole pattern also matches the empty path.
				dotFree := dotFreeFrom[valueIndex]
				remaining := valueIndex < len(value)
				soleGlobstar := patternIndex == 0 && len(pattern) == 1
				result = dotFree && (remaining || soleGlobstar)
				break
			}
			// A globstar between segments may consume nothing, which lets
			// "a/**/b" match "a/b".
			if match(patternIndex+1, valueIndex) {
				result = true
			} else if valueIndex < len(value) && !isDotSegment(value[valueIndex]) {
				result = match(patternIndex, valueIndex+1)
			}
		default:
			// The empty segment that marks a leading slash is only matched by an
			// absolute pattern (or consumed by a globstar, above).
			root := valueIndex == 0 && valueIndex < len(value) && value[0] == "" && pattern[patternIndex] != ""
			result = !root && valueIndex < len(value) &&
				matchGlobSegment(pattern[patternIndex], value[valueIndex], nocase) &&
				match(patternIndex+1, valueIndex+1)
		}
		seen[key] = true
		memo[key] = result
		return result
	}
	return match(0, 0)
}

// isDotSegment reports a path segment minimatch's globstar will not consume.
func isDotSegment(segment string) bool {
	return strings.HasPrefix(segment, ".")
}

// matchGlobSegment matches one pattern segment against one value segment.
func matchGlobSegment(pattern, value string, nocase bool) bool {
	// minimatch's parser turns a class that holds only a dot into a literal dot.
	if strings.HasPrefix(pattern, "[.]") {
		pattern = "." + pattern[len("[.]"):]
	}
	// A wildcard segment needs at least one character (only a trailing-separator
	// segment, spelled "", matches an empty one).
	if value == "" && pattern != "" {
		return false
	}
	// minimatch never matches the "." or ".." path segments with anything but
	// that literal segment.
	if value == "." || value == ".." {
		return strings.ReplaceAll(pattern, "[.]", ".") == value
	}
	p := []rune(pattern)
	s := []rune(value)
	if nocase {
		for index := range p {
			p[index] = toLowerRune(p[index])
		}
		for index := range s {
			s[index] = toLowerRune(s[index])
		}
	}
	// The dotfile rule: a wildcard at the start of a segment does not match a
	// leading "." (a literal dot in the pattern still does).
	if len(s) > 0 && s[0] == '.' && len(p) > 0 {
		switch p[0] {
		case '*', '?', '[':
			return false
		}
	}
	return matchSegmentHere(p, s)
}

func toLowerRune(char rune) rune {
	if char >= 'A' && char <= 'Z' {
		return char + ('a' - 'A')
	}
	return char
}

// matchSegmentHere matches a single path segment; it never sees a "/". The
// recursion is memoized on (pattern, value) offsets, so a pattern like
// "*a*a*...*b" cannot backtrack exponentially.
func matchSegmentHere(pattern, value []rune) bool {
	type state struct{ pattern, value int }
	// 0 is "not computed yet", 1 is false, 2 is true.
	memo := make(map[state]uint8)
	var match func(patternIndex, valueIndex int) bool
	match = func(patternIndex, valueIndex int) bool {
		key := state{patternIndex, valueIndex}
		if cached := memo[key]; cached != 0 {
			return cached == 2
		}
		result := false
		switch {
		case patternIndex == len(pattern):
			result = valueIndex == len(value)
		case pattern[patternIndex] == '*':
			// Adjacent stars inside a segment are a single star (minimatch treats
			// `a**b` as `a*b`).
			next := patternIndex + 1
			for next < len(pattern) && pattern[next] == '*' {
				next++
			}
			result = match(next, valueIndex) ||
				(valueIndex < len(value) && match(patternIndex, valueIndex+1))
		case pattern[patternIndex] == '?':
			result = valueIndex < len(value) && match(patternIndex+1, valueIndex+1)
		case pattern[patternIndex] == '[':
			if valueIndex < len(value) {
				consumed, ok := matchClass(pattern[patternIndex:], value[valueIndex])
				result = ok && match(patternIndex+consumed, valueIndex+1)
			}
		case pattern[patternIndex] == '\\':
			escaped := patternIndex + 1
			if escaped >= len(pattern) {
				escaped = patternIndex
			}
			result = valueIndex < len(value) && value[valueIndex] == pattern[escaped] &&
				match(escaped+1, valueIndex+1)
		default:
			result = valueIndex < len(value) && value[valueIndex] == pattern[patternIndex] &&
				match(patternIndex+1, valueIndex+1)
		}
		if result {
			memo[key] = 2
		} else {
			memo[key] = 1
		}
		return result
	}
	return match(0, 0)
}

// matchClass matches one character against a character class, returning the
// number of pattern runes the class consumed.
func matchClass(pattern []rune, char rune) (int, bool) {
	index := 1
	negated := false
	if index < len(pattern) && (pattern[index] == '!' || pattern[index] == '^') {
		negated = true
		index++
	}
	matched := false
	valid := false
	first := true
	for index < len(pattern) && (pattern[index] != ']' || first) {
		first = false
		low := pattern[index]
		if low == '\\' && index+1 < len(pattern) {
			index++
			low = pattern[index]
		}
		index++
		if index+1 < len(pattern) && pattern[index] == '-' && pattern[index+1] != ']' {
			high := pattern[index+1]
			index += 2
			if high == '\\' && index < len(pattern) {
				high = pattern[index]
				index++
			}
			if low <= char && char <= high {
				matched = true
			}
			valid = valid || low <= high
			continue
		}
		valid = true
		if low == char {
			matched = true
		}
	}
	if index >= len(pattern) {
		// Unterminated class: treat "[" as a literal.
		return 1, char == '['
	}
	consumed := index + 1
	return consumed, valid && matched != negated
}
