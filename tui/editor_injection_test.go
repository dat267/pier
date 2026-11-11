package tui

import (
	"strings"
	"testing"
)

// TestEditorSanitizesTextOnEntry pins the editor against terminal control
// injection through its buffer (D452, TODO A503). The editor draws its buffer,
// and text can arrive from a paste or a completion rather than a keystroke, which
// never carries a raw escape byte. Reference: pi v1.5.0 f9a26365 editor.ts
// inserts the text it is given, as the port did.
func TestEditorSanitizesTextOnEntry(t *testing.T) {
	payload := "hi \x1b[2J\x1b]52;c;cGF3bmVk\x07 there"

	// A completion is the path that carried a file name from disk into the
	// buffer: the provider returns whole lines, so the result is installed by the
	// same guard the Tab and list paths use.
	completion := CompletionResult{Lines: []string{payload}, CursorLine: 0, CursorCol: len(payload)}
	cases := map[string]func(*Editor){
		"SetText":            func(e *Editor) { e.SetText(payload) },
		"InsertTextAtCursor": func(e *Editor) { e.InsertTextAtCursor(payload) },
		"paste":              func(e *Editor) { e.handlePasteLocked(payload) },
		"completion":         func(e *Editor) { e.applyCompletionResultLocked(completion) },
	}
	for name, fill := range cases {
		t.Run(name, func(t *testing.T) {
			editor := NewEditor(&editorHost{rows: 10}, editorTestTheme(), EditorOptions{})
			fill(editor)
			for _, line := range editor.Render(100) {
				if strings.Contains(line, "\x1b[2J") || strings.Contains(line, "\x1b]52") {
					t.Fatalf("the editor drew a stored sequence: %q", line)
				}
			}
		})
	}
}
