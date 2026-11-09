package tui

import "testing"

type undoStepHost struct{}

func (undoStepHost) Rows() int          { return 24 }
func (undoStepHost) RequestRender(bool) {}

// isWhitespaceChar in pi v1.5.0 (f9a26365) packages/tui/src/utils.ts is
// `/\s/.test(char)`: any string that contains a whitespace character counts.
// The editor's undo coalescing uses it, so an insertion whose text contains a
// space starts a new undo unit, while one without whitespace coalesces into the
// current typing unit. Pier tested "every rune is whitespace", which is only
// true for a lone space, so a multi-character insertion could join the previous
// unit and one undo removed an extra typing step. Reference behaviour, with the
// input string inserted through HandleInput and then ctrl+- (undo):
//
//	["a", "first message", undo] -> "a" (the insert is its own unit)
//	["a", "bc", undo]            -> ""  (coalesced with "a")
//	["a", " b", undo]            -> "a"
func TestEditorMultiCharacterInsertHasItsOwnUndoUnit(t *testing.T) {
	for _, test := range []struct {
		keys []string
		want string
	}{
		{[]string{"a", "first message", "\x1f"}, "a"},
		{[]string{"a", "bc", "\x1f"}, ""},
		{[]string{"a", " b", "\x1f"}, "a"},
		{[]string{"漢", "first message", "\x1f"}, "漢"},
	} {
		editor := NewEditor(undoStepHost{}, EditorTheme{}, EditorOptions{})
		for _, key := range test.keys {
			editor.HandleInput(key)
		}
		if got := editor.GetText(); got != test.want {
			t.Errorf("keys %q: text %q, want %q", test.keys, got, test.want)
		}
	}
}
