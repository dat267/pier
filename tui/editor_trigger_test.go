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
