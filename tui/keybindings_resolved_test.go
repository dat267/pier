package tui

import "testing"

// pi v1.5.2 (b4afdddf), packages/tui/src/keybindings.ts getResolvedBindings
// returns `keys.length === 1 ? keys[0] : [...keys]`, so a single-key binding is a
// bare string and a multi-key or unset binding is an array.
func TestResolvedBindingsShape(t *testing.T) {
	manager := NewKeybindingsManager(TUIKeybindings, nil)
	resolved := manager.GetResolvedBindings()

	single, ok := resolved["tui.editor.cursorUp"].(string)
	if !ok || single != "up" {
		t.Fatalf("cursorUp = %#v, want the bare string \"up\"", resolved["tui.editor.cursorUp"])
	}
	multiple, ok := resolved["tui.editor.cursorLeft"].([]string)
	if !ok || len(multiple) != 2 || multiple[0] != "left" || multiple[1] != "ctrl+b" {
		t.Fatalf("cursorLeft = %#v, want the two keys", resolved["tui.editor.cursorLeft"])
	}
	unset, ok := resolved["tui.editor.historyPrevious"].([]string)
	if !ok || len(unset) != 0 {
		t.Fatalf("historyPrevious = %#v, want an empty array", resolved["tui.editor.historyPrevious"])
	}
	if len(resolved) != len(TUIKeybindings) {
		t.Fatalf("resolved %d bindings, want %d", len(resolved), len(TUIKeybindings))
	}
}
