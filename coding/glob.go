package coding

import "strings"

// MatchGlob matches a value against a minimatch-style pattern as used by
// model-scope resolution (upstream uses minimatch with { nocase: true }).
//
// Supported syntax: `*` (any run of characters except "/"), a whole-segment `**`
// (any run of path segments, never crossing into a segment that starts with
// "."), `?` (one character except "/"), `[...]` character classes with ranges
// and `!`/`^` negation, `{a,b}` brace expansion, and `\` escaping.
//
// D22: minimatch's extended globs (`!(...)`, `+(...)`, `?(...)`, `@(...)`,
// `*(...)`), extglob negation patterns (`!pattern`), its POSIX character
// classes, `\`-escaped separators, and its `matchBase`/`nocase` options beyond
// ASCII case folding are not implemented; model ids never use them. D262 covers
// the remaining differences, which are four pattern shapes model ids cannot
// reach.
func MatchGlob(pattern, value string, nocase bool) bool {
	// An empty pattern matches only the empty path (minimatch does not treat "/"
	// as an empty match).
	if pattern == "" {
		return value == ""
	}
	for _, expanded := range expandBraces(pattern) {
		if matchGlobSegments(expanded, value, nocase) {
			return true
		}
	}
	return false
}

// expandBraces expands the first brace group into alternatives, recursively.
func expandBraces(pattern string) []string {
	open := -1
	depth := 0
	for index, char := range pattern {
		switch char {
		case '\\':
			continue
		case '{':
			if depth == 0 {
				open = index
			}
			depth++
		case '}':
			if depth > 0 {
				depth--
				if depth == 0 {
					prefix := pattern[:open]
					body := pattern[open+1 : index]
					suffix := pattern[index+1:]
					var out []string
					for _, alternative := range splitTopLevel(body) {
						for _, expanded := range expandBraces(prefix + alternative + suffix) {
							out = append(out, expanded)
						}
					}
					return out
				}
			}
		}
	}
	return []string{pattern}
}

func splitTopLevel(body string) []string {
	var parts []string
	depth := 0
	current := strings.Builder{}
	for _, char := range body {
		switch char {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, current.String())
				current.Reset()
				continue
			}
		}
		current.WriteRune(char)
	}
	parts = append(parts, current.String())
	return parts
}

func matchGlobSegments(pattern, value string, nocase bool) bool {
	return matchSegmentList(globSegments(pattern), globSegments(value), nocase)
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
	memo := make(map[state]bool, len(pattern)*len(value)+1)
	seen := make(map[state]bool, len(memo))
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

// matchSegmentHere matches a single path segment; it never sees a "/".
func matchSegmentHere(pattern, value []rune) bool {
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			// Adjacent stars inside a segment are a single star (minimatch treats
			// `a**b` as `a*b`).
			rest := pattern[1:]
			for len(rest) > 0 && rest[0] == '*' {
				rest = rest[1:]
			}
			for index := 0; index <= len(value); index++ {
				if matchSegmentHere(rest, value[index:]) {
					return true
				}
			}
			return false
		case '?':
			if len(value) == 0 {
				return false
			}
			pattern = pattern[1:]
			value = value[1:]
		case '[':
			if len(value) == 0 {
				return false
			}
			consumed, ok := matchClass(pattern, value[0])
			if !ok {
				return false
			}
			pattern = pattern[consumed:]
			value = value[1:]
		case '\\':
			if len(pattern) > 1 {
				pattern = pattern[1:]
			}
			if len(value) == 0 || value[0] != pattern[0] {
				return false
			}
			pattern = pattern[1:]
			value = value[1:]
		default:
			if len(value) == 0 || value[0] != pattern[0] {
				return false
			}
			pattern = pattern[1:]
			value = value[1:]
		}
	}
	return len(value) == 0
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
	first := true
	for index < len(pattern) && (pattern[index] != ']' || first) {
		first = false
		if index+2 < len(pattern) && pattern[index+1] == '-' && pattern[index+2] != ']' {
			low, high := pattern[index], pattern[index+2]
			if low <= char && char <= high {
				matched = true
			}
			index += 3
			continue
		}
		if pattern[index] == char {
			matched = true
		}
		index++
	}
	if index >= len(pattern) {
		// Unterminated class: treat "[" as a literal.
		return 1, char == '['
	}
	consumed := index + 1
	return consumed, matched != negated
}
