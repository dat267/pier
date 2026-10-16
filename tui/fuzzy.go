package tui

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"
)

// Port of src/fuzzy.ts: subsequence fuzzy matching with a quality score
// (lower is better) and token-based filtering.

// FuzzyMatch is a fuzzy match outcome.
type FuzzyMatch struct {
	Matches bool
	Score   float64
}

// MatchFuzzy matches query characters in order within text. Upstream names
// both the type and the function fuzzyMatch/FuzzyMatch; Go cannot, so the
// function is MatchFuzzy (divergence D66).
func MatchFuzzy(query string, text string) FuzzyMatch {
	if isASCII(query) && isASCII(text) {
		// ASCII byte offsets equal UTF-16 code-unit offsets, so lowercase bytes
		// on demand and avoid building normalized strings and unit slices.
		primaryMatch := matchFuzzyASCII(query, text)
		if primaryMatch.Matches || !asciiAlphaNumericSwapCandidate(query) {
			return primaryMatch
		}
	}

	queryLower := jsLower(query)
	textLower := jsLower(text)

	matchQuery := func(normalizedQuery string) FuzzyMatch {
		// Upstream indexes the lowercased strings by UTF-16 code units
		// (String.indexOf and text[i-1]), so the score uses code-unit offsets.
		queryUnits := utf16.Encode([]rune(normalizedQuery))
		textUnits := utf16.Encode([]rune(textLower))
		if len(queryUnits) == 0 {
			return FuzzyMatch{Matches: true, Score: 0}
		}
		if len(queryUnits) > len(textUnits) {
			return FuzzyMatch{Matches: false, Score: 0}
		}

		queryIndex := 0
		score := 0.0
		lastMatchIndex := -1
		consecutiveMatches := 0

		for queryIndex < len(queryUnits) {
			i := indexUnitFrom(textUnits, queryUnits[queryIndex], lastMatchIndex+1)
			if i == -1 {
				break
			}

			isWordBoundary := i == 0 || isFuzzyBoundaryUnit(textUnits[i-1])

			// Reward consecutive matches.
			if lastMatchIndex == i-1 {
				consecutiveMatches++
				score -= float64(consecutiveMatches) * 5
			} else {
				consecutiveMatches = 0
				// Penalize gaps.
				if lastMatchIndex >= 0 {
					score += float64(i-lastMatchIndex-1) * 2
				}
			}

			// Reward word-boundary matches.
			if isWordBoundary {
				score -= 10
			}

			// Slight penalty for later matches.
			score += unfusedProduct(float64(i), 0.1)

			lastMatchIndex = i
			queryIndex++
		}

		if queryIndex < len(queryUnits) {
			return FuzzyMatch{Matches: false, Score: 0}
		}
		if normalizedQuery == textLower {
			score -= 100
		}
		return FuzzyMatch{Matches: true, Score: score}
	}

	primaryMatch := matchQuery(queryLower)
	if primaryMatch.Matches {
		return primaryMatch
	}

	swappedQuery := swappedAlphaNumericQuery(queryLower)
	if swappedQuery == "" {
		return primaryMatch
	}

	swappedMatch := matchQuery(swappedQuery)
	if !swappedMatch.Matches {
		return primaryMatch
	}
	return FuzzyMatch{Matches: true, Score: swappedMatch.Score + 5}
}

// unfusedProduct returns a*b with the multiplication rounded before it is
// accumulated.
//
// Go's arm64 and ppc64 backends contract `acc += a*b` into a fused
// multiply-add, which rounds once, where V8 rounds the product and the sum
// separately. The ULP that fusion saves is enough to drift from upstream's
// golden scores, so the product is forced through a call boundary — //go:noinline
// keeps the intermediate rounding and the port bit-identical to V8.
//
//go:noinline
func unfusedProduct(a, b float64) float64 { return a * b }

var (
	alphaNumericQueryRegex = regexp.MustCompile(`^([a-z]+)([0-9]+)$`)
	numericAlphaQueryRegex = regexp.MustCompile(`^([0-9]+)([a-z]+)$`)
)

// swappedAlphaNumericQuery swaps a leading letter/digit run with a trailing
// one (upstream's alphaNumericMatch/numericAlphaMatch).
func swappedAlphaNumericQuery(query string) string {
	if match := alphaNumericQueryRegex.FindStringSubmatch(query); match != nil {
		return match[2] + match[1]
	}
	if match := numericAlphaQueryRegex.FindStringSubmatch(query); match != nil {
		return match[2] + match[1]
	}
	return ""
}

// isFuzzyBoundaryUnit reports the upstream `/[\s\-_./:]/` word-boundary set.
func isFuzzyBoundaryUnit(unit uint16) bool {
	if isWhitespaceRune(rune(unit)) {
		return true
	}
	switch unit {
	case '-', '_', '.', '/', ':':
		return true
	}
	return false
}

func lowerASCIIByte(value byte) byte {
	if value >= 'A' && value <= 'Z' {
		return value + ('a' - 'A')
	}
	return value
}

func indexASCIILowerFrom(text string, needle byte, start int) int {
	for index := max(0, start); index < len(text); index++ {
		if lowerASCIIByte(text[index]) == needle {
			return index
		}
	}
	return -1
}

func matchFuzzyASCII(query string, text string) FuzzyMatch {
	if len(query) == 0 {
		return FuzzyMatch{Matches: true, Score: 0}
	}
	if len(query) > len(text) {
		return FuzzyMatch{Matches: false, Score: 0}
	}

	queryIndex := 0
	score := 0.0
	lastMatchIndex := -1
	consecutiveMatches := 0
	for queryIndex < len(query) {
		i := indexASCIILowerFrom(text, lowerASCIIByte(query[queryIndex]), lastMatchIndex+1)
		if i == -1 {
			break
		}

		isWordBoundary := i == 0 || isFuzzyBoundaryUnit(uint16(lowerASCIIByte(text[i-1])))
		if lastMatchIndex == i-1 {
			consecutiveMatches++
			score -= float64(consecutiveMatches) * 5
		} else {
			consecutiveMatches = 0
			if lastMatchIndex >= 0 {
				score += float64(i-lastMatchIndex-1) * 2
			}
		}
		if isWordBoundary {
			score -= 10
		}
		score += unfusedProduct(float64(i), 0.1)
		lastMatchIndex = i
		queryIndex++
	}

	if queryIndex < len(query) {
		return FuzzyMatch{Matches: false, Score: 0}
	}
	if asciiEqualFold(query, text) {
		score -= 100
	}
	return FuzzyMatch{Matches: true, Score: score}
}

func asciiEqualFold(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if lowerASCIIByte(left[index]) != lowerASCIIByte(right[index]) {
			return false
		}
	}
	return true
}

// asciiAlphaNumericSwapCandidate mirrors the two upstream swap patterns
// without allocating. It is called only for ASCII queries.
func asciiAlphaNumericSwapCandidate(query string) bool {
	lettersEnd := 0
	for lettersEnd < len(query) {
		value := lowerASCIIByte(query[lettersEnd])
		if value < 'a' || value > 'z' {
			break
		}
		lettersEnd++
	}
	if lettersEnd > 0 && lettersEnd < len(query) {
		for index := lettersEnd; index < len(query); index++ {
			if query[index] < '0' || query[index] > '9' {
				return false
			}
		}
		return true
	}

	digitsEnd := 0
	for digitsEnd < len(query) && query[digitsEnd] >= '0' && query[digitsEnd] <= '9' {
		digitsEnd++
	}
	if digitsEnd > 0 && digitsEnd < len(query) {
		for index := digitsEnd; index < len(query); index++ {
			value := lowerASCIIByte(query[index])
			if value < 'a' || value > 'z' {
				return false
			}
		}
		return true
	}
	return false
}

// indexUnitFrom returns the index of needle at or after start.
func indexUnitFrom(haystack []uint16, needle uint16, start int) int {
	if start < 0 {
		start = 0
	}
	for i := start; i < len(haystack); i++ {
		if haystack[i] == needle {
			return i
		}
	}
	return -1
}

// FuzzyFilter filters and sorts items by fuzzy match quality (best first).
// Whitespace- and slash-separated tokens must all match.
func FuzzyFilter[T any](items []T, query string, getText func(item T) string) []T {
	if jsTrim(query) == "" {
		return items
	}

	tokens := splitFuzzyTokens(jsTrim(query))
	if len(tokens) == 0 {
		return items
	}

	type result struct {
		item  T
		score float64
	}
	var results []result
	for _, item := range items {
		text := getText(item)
		totalScore := 0.0
		allMatch := true
		for _, token := range tokens {
			match := MatchFuzzy(token, text)
			if match.Matches {
				totalScore += match.Score
			} else {
				allMatch = false
				break
			}
		}
		if allMatch {
			results = append(results, result{item: item, score: totalScore})
		}
	}

	sort.SliceStable(results, func(a, b int) bool { return results[a].score < results[b].score })
	out := make([]T, 0, len(results))
	for _, entry := range results {
		out = append(out, entry.item)
	}
	return out
}

// splitFuzzyTokens splits on whitespace and slashes.
func splitFuzzyTokens(query string) []string {
	return strings.FieldsFunc(query, func(r rune) bool {
		return isWhitespaceRune(r) || r == '/'
	})
}
