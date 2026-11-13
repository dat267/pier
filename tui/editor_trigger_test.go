package tui

import (
	"context"
	"testing"
)

// astralTriggerProvider offers an astral character as a trigger.
type astralTriggerProvider struct{}

func (p *astralTriggerProvider) TriggerCharacters() []string { return []string{"\U0001F600"} }

func (p *astralTriggerProvider) GetSuggestions(context.Context, []string, int, int, bool) *AutocompleteSuggestions {
	return nil
}

func (p *astralTriggerProvider) ApplyCompletion([]string, int, int, AutocompleteItem, string) CompletionResult {
	return CompletionResult{}
}

// Upstream accepts a trigger character only when String.length === 1, so an
// astral character (two UTF-16 code units) is rejected.
func TestEditorRejectsAstralAutocompleteTrigger(t *testing.T) {
	editor := NewEditor(&editorHost{rows: 10}, editorTestTheme(), EditorOptions{})
	editor.SetAutocompleteProvider(&astralTriggerProvider{})
	for _, trigger := range editor.autocompleteTriggers {
		if trigger == "\U0001F600" {
			t.Fatal("astral trigger character was accepted")
		}
	}
}

// pi v1.5.2 (b4afdddf) editor.ts:1253 tests the character with an unanchored
// regex, `/[a-zA-Z0-9.\-_]/.test(char) || cjkBreakRegex.test(char)`, so a word
// character anywhere in the string counts. The port looped over the runes but
// returned on the first one, so only a leading word character or a CJK break
// anywhere was recognised.
func TestAutocompleteWordCharChecksEveryRune(t *testing.T) {
	cases := map[string]bool{
		"a":  true,
		"1":  true,
		".":  true,
		"-":  true,
		"_":  true,
		"漢":  true, // CJK break
		"!":  false,
		"":   false,
		"!a": true, // a word character later in the string
		" a": true,
	}
	for input, want := range cases {
		if got := isAutocompleteWordChar(input); got != want {
			t.Errorf("isAutocompleteWordChar(%q) = %v, want %v", input, got, want)
		}
	}
}
