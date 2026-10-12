package tui

import (
	"context"
	"regexp"
	"unicode"
)

func unicodePunctuation(r rune) bool { return unicode.IsPunct(r) }

// Port of the autocomplete surface from src/autocomplete.ts that the editor
// consumes. The provider implementations (combined slash-command and file
// completion) are ported separately.

// AutocompleteItem is a completion candidate.
type AutocompleteItem struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// AutocompleteSuggestions is a completion result set.
type AutocompleteSuggestions struct {
	Items  []AutocompleteItem
	Prefix string
}

// GetArgumentCompletions returns argument completions, or nil.

// CompletionResult is the text state after applying a completion.
type CompletionResult struct {
	Lines      []string
	CursorLine int
	CursorCol  int
}

// AutocompleteProvider supplies completions for the editor.
type AutocompleteProvider interface {
	// TriggerCharacters are the characters that trigger this provider at token
	// boundaries.
	TriggerCharacters() []string
	// GetSuggestions returns completions for the cursor position, or nil.
	// The context carries cancellation (upstream's AbortSignal).
	GetSuggestions(ctx context.Context, lines []string, cursorLine int, cursorCol int, force bool) *AutocompleteSuggestions
	// ApplyCompletion applies the selected item.
	ApplyCompletion(lines []string, cursorLine int, cursorCol int, item AutocompleteItem, prefix string) CompletionResult
}

// FileCompletionTrigger is the optional provider hook deciding whether an
// explicit Tab completion may trigger file completion.
type FileCompletionTrigger interface {
	ShouldTriggerFileCompletion(lines []string, cursorLine int, cursorCol int) bool
}

// ---- Text classification from src/utils.ts ----

// cjkPunctuation is the explicit CJK punctuation set from utils.ts.
var editorCJKPunctuation = map[rune]bool{
	'，': true, '．': true, '：': true, '；': true, '！': true, '？': true,
	'（': true, '）': true, '［': true, '］': true, '｛': true, '｝': true,
	'“': true, '”': true, '‘': true, '’': true, '…': true, '—': true,
}

// isAutocompleteSeparator reports whether the character separates a completion
// token from the preceding text (upstream's autocompleteSeparatorRegex).
// Upstream's cjkPunctuationRegex is `(?:(?=\p{Punctuation})cjkBreakRegex|
// [explicit list])`: the lookahead and cjkBreakRegex both apply to the same
// character, so a punctuation character that is itself a CJK-script character
// separates, and the next character is irrelevant.
func isAutocompleteSeparator(character rune) bool {
	if isWhitespaceRune(character) {
		return true
	}
	if editorCJKPunctuation[character] {
		return true
	}
	return unicodePunctuation(character) && isCJKPunctuation(character)
}

// cjkPunctuationRanges are the punctuation code points whose Unicode
// Script_Extensions include a CJK script (Han, Hiragana, Katakana, Hangul or
// Bopomofo), excluding the explicit list in editorCJKPunctuation. Upstream
// expresses this as `(?=\p{Punctuation})[\p{Script_Extensions=...}]`, which Go
// regexp cannot match because it has no Script_Extensions class; the ranges
// were enumerated from the reference regex.
var cjkPunctuationRanges = [][2]rune{
	{0x00B7, 0x00B7}, {0x3001, 0x3003}, {0x3008, 0x301D}, {0x3014, 0x301F},
	{0x3030, 0x3030}, {0x303D, 0x303D}, {0x30A0, 0x30A0}, {0x30FB, 0x30FB},
	{0xFE45, 0xFE46}, {0xFF61, 0xFF65}, {0x16FE2, 0x16FE2},
}

func isCJKPunctuation(character rune) bool {
	for _, tableRange := range cjkPunctuationRanges {
		if character >= tableRange[0] && character <= tableRange[1] {
			return true
		}
	}
	return false
}

// mustCompileRegex compiles a pattern that is known to be valid.
func mustCompileRegex(pattern string) *regexp.Regexp { return regexp.MustCompile(pattern) }
