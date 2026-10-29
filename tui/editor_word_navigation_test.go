package tui

import (
	"testing"
)

// pi v1.4.0 (4a080252), packages/tui/src/word-navigation.ts: findWordBackward
// and findWordForward segment only the text before or after the cursor, so a
// cursor inside a word moves to that word's edge. Segmenting the whole line
// made the backward move land before the word start (a negative column, which
// the next edit or forward delete sliced with) and left the forward move with
// nothing to advance over (CR-210).
func TestEditorWordNavigationInsideWord(t *testing.T) {
	newEditor := func(text string) *Editor {
		editor := NewEditor(&editorHost{rows: 10}, editorTestTheme(), EditorOptions{})
		editor.SetFocused(true)
		editor.SetText(text)
		return editor
	}
	// press sends inputs and then types X so the cursor position is observable
	// through the text.
	press := func(editor *Editor, inputs ...string) string {
		for _, input := range inputs {
			editor.HandleInput(input)
		}
		return editor.GetText()
	}

	t.Run("backward stops at word start", func(t *testing.T) {
		editor := newEditor("ab")
		if got := press(editor, "\x1b[D", "\x1b[1;5D", "X"); got != "Xab" {
			t.Fatalf("text = %q, want %q", got, "Xab")
		}
	})

	t.Run("backward then forward delete", func(t *testing.T) {
		editor := newEditor("ab")
		if got := press(editor, "\x1b[D", "\x1b[1;5D", "\x1b[3~"); got != "b" {
			t.Fatalf("text = %q, want %q", got, "b")
		}
	})

	t.Run("forward stops at word end", func(t *testing.T) {
		editor := newEditor("abcd")
		// Home, then right once: the cursor sits inside the word.
		if got := press(editor, "\x1b[H", "\x1b[C", "\x1b[1;5C", "X"); got != "abcdX" {
			t.Fatalf("text = %q, want %q", got, "abcdX")
		}
	})

	t.Run("forward from punctuation boundary", func(t *testing.T) {
		editor := newEditor("a.b")
		if got := press(editor, "\x1b[D", "\x1b[D", "\x1b[1;5C", "X"); got != "a.Xb" {
			t.Fatalf("text = %q, want %q", got, "a.Xb")
		}
	})

	t.Run("backward inside a later word", func(t *testing.T) {
		editor := newEditor("foo bar")
		if got := press(editor, "\x1b[D", "\x1b[D", "\x1b[1;5D", "X"); got != "foo Xbar" {
			t.Fatalf("text = %q, want %q", got, "foo Xbar")
		}
	})

	t.Run("cursor never goes negative on multibyte lines", func(t *testing.T) {
		editor := newEditor("a\u0301b")
		editor.HandleInput("\x1b[H")
		editor.HandleInput("\x1b[1;5C")
		editor.HandleInput("X")
		if got := editor.GetText(); got != "a\u0301bX" {
			t.Fatalf("text = %q, want %q", got, "a\u0301bX")
		}
	})
}
