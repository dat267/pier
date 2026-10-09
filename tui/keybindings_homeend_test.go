package tui

import "testing"

// Home/End belong to the editor, and the transcript's top/bottom moved to Ctrl+Home/Ctrl+End,
// which no longer move the editor cursor (upstream 1.0.3, #10314). Before this the editor claimed
// Ctrl+Home and Ctrl+End, and no key at all scrolled the transcript.
func TestHomeEndBindingsFollowUpstream(t *testing.T) {
	kb := GetKeybindings()
	cases := []struct {
		input string
		name  string
		want  bool
	}{
		// Matches takes the terminal's raw sequence, the way the editor calls it.
		{"\x1b[H", "tui.editor.cursorLineStart", true},
		{"\x1b[F", "tui.editor.cursorLineEnd", true},
		{"\x01", "tui.editor.cursorLineStart", true},
		{"\x05", "tui.editor.cursorLineEnd", true},
		{"\x1b[7^", "tui.editor.cursorLineStart", false},
		{"\x1b[8^", "tui.editor.cursorLineEnd", false},
		{"\x1b[7^", "tui.altScreen.top", true},
		{"\x1b[8^", "tui.altScreen.bottom", true},
		{"\x1b[H", "tui.altScreen.top", false},
		{"\x1b[F", "tui.altScreen.bottom", false},
	}
	for _, testCase := range cases {
		if got := kb.Matches(testCase.input, testCase.name); got != testCase.want {
			t.Errorf("Matches(%q, %q) = %v, want %v", testCase.input, testCase.name, got, testCase.want)
		}
	}
}
