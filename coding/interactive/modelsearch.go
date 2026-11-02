package interactive

// Port of src/modes/interactive/model-search.ts: the search text and the
// distinct-name rule used by the model selectors.

import (
	"strings"
	"unicode"

	"github.com/dat267/pier/ai"
)

// ModelSearchItem is a model entry for search text generation.
type ModelSearchItem struct {
	ID       string
	Provider string
	Name     string
	HasName  bool
}

// GetModelSearchText builds the default search text.
func GetModelSearchText(item ModelSearchItem) string {
	name := ""
	if item.HasName {
		name = " " + item.Name
	}
	return item.ID + " " + item.Provider + " " + item.Provider + "/" + item.ID + " " + item.Provider + " " + item.ID + name
}

// GetModelSelectorSearchText builds the selector search text: the /model
// selector search should rank exact provider-prefixed queries before
// proxy-provider IDs like openrouter/openai/gpt-5, so the bare model ID is
// kept out of the leading position.
func GetModelSelectorSearchText(item ModelSearchItem) string {
	name := ""
	if item.HasName {
		name = " " + item.Name
	}
	return item.Provider + " " + item.Provider + "/" + item.ID + " " + item.Provider + " " + item.ID + name
}

// GetDistinctModelName returns the display name when it has words the id does
// not, e.g. a renamed override ("MiniMax M3 [rasmus@openrouter, parasail]" for
// "minimax/minimax-m3"). Names that only restate the id with different casing,
// separators, or a dropped namespace/version suffix return ok=false
// (pi v1.5.0 model-search.ts getDistinctModelName).
func GetDistinctModelName(id, name string) (string, bool) {
	trimmed := ai.JSTrim(name)
	if trimmed == "" {
		return "", false
	}
	idWords := splitModelLabel(id)
	tokens := make(map[string]bool, len(idWords))
	for _, word := range idWords {
		tokens[word] = true
	}
	compactID := strings.Join(idWords, "")
	covered := func(word string) bool {
		if tokens[word] {
			return true
		}
		// Single-character words must match a whole id token.
		return len([]rune(word)) > 1 && strings.Contains(compactID, word)
	}
	for _, word := range splitModelLabel(trimmed) {
		if !covered(word) {
			return trimmed, true
		}
	}
	return "", false
}

// splitModelLabel lowercases value and splits it on every run of characters
// that are neither letters nor numbers (JS /[^\p{L}\p{N}]+/u).
func splitModelLabel(value string) []string {
	lower := ai.JSLower(value)
	var words []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			words = append(words, current.String())
			current.Reset()
		}
	}
	for _, r := range lower {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			current.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return words
}
