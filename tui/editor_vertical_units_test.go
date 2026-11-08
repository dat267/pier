package tui

import "testing"

// Upstream's sticky column is cursorCol - startCol over JavaScript strings, a
// UTF-16 offset, so vertical moves keep the code-unit column rather than the
// byte offset Go stores (pi v1.5.0 packages/tui/src/components/editor.ts
// moveToVisualLine). Expected cursors come from the upstream Editor and are
// translated to byte offsets.
func TestEditorVerticalMovesKeepTheUTF16Column(t *testing.T) {
	for _, tc := range []struct {
		name      string
		text      string
		inputs    []string
		line, col int
	}{
		// "c😀" ends at unit 3 (byte 5); PageUp lands at "a  |  b".
		{"page up from emoji", "", []string{"\x1b[200~a\tb\r\nc\x1b[201~", "😀", "\x1b[5~"}, 0, 3},
		{"up from emoji", "abcdef\nc😀", []string{"\x1b[A"}, 0, 3},
		// Unit 3 is inside the two-unit "é́", which snaps to its start (byte 4).
		{"up into combining marks", "é́é́é́é́\nxyz", []string{"\x1b[A"}, 0, 4},
		// "é́é́" ends at unit 4 (byte 8).
		{"up from combining marks", "abcdef\né́é́", []string{"\x1b[A"}, 0, 4},
	} {
		editor := NewEditor(&editorHost{rows: 10}, editorTestTheme(), EditorOptions{})
		editor.SetFocused(true)
		if tc.text != "" {
			editor.SetText(tc.text)
		}
		for _, input := range tc.inputs {
			editor.HandleInput(input)
		}
		editor.Render(30)
		if line, col := editor.Cursor(); line != tc.line || col != tc.col {
			t.Errorf("%s: cursor %d,%d; want %d,%d", tc.name, line, col, tc.line, tc.col)
		}
	}
}
