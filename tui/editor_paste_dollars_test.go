package tui

import (
	"strings"
	"testing"
)

// pi v1.4.0 (4a080252), components/editor.ts expandPasteMarkers passes a
// replacement function to String.replace, so dollar sequences remain literal.
func TestEditorLargePastePreservesDollarTextOnSubmit(t *testing.T) {
	previous := GetKeybindings()
	SetKeybindings(NewKeybindingsManager(TUIKeybindings, nil))
	t.Cleanup(func() { SetKeybindings(previous) })
	editor := NewEditor(nil, EditorTheme{}, EditorOptions{})
	paste := strings.Repeat("line\n", 10) + `echo "$HOME" "$1" "${name}" "$$"`
	editor.HandleInput("\x1b[200~" + paste + "\x1b[201~")
	if got := editor.GetExpandedText(); got != paste {
		t.Errorf("expanded paste changed dollar sequences: got %q, want %q", got, paste)
	}
	var submitted string
	editor.OnSubmit = func(text string) { submitted = text }
	editor.HandleInput("\r")
	if submitted != paste {
		t.Errorf("submitted paste changed dollar sequences: got %q, want %q", submitted, paste)
	}
}
