package tui

// pi v1.5.0 tui-alt-screen.ts: `shouldDeferViewportInput` also defers when the
// focused component captures viewport input, so an open list or an editor with
// an open autocomplete menu receives PageUp/PageDown instead of the transcript.

import "testing"

func TestAltScreenDefersViewportInputToCapturingFocus(t *testing.T) {
	screen := NewAltScreen(&recordingTerminal{width: 40, height: 10}, false, t.TempDir(), AltScreenOptions{})
	list := NewSelectList(nil, 5, SelectListTheme{}, SelectListLayoutOptions{})
	screen.SetFocus(list)
	if !screen.shouldDeferViewportInputToOverlayLocked() {
		t.Fatal("a focused select list must capture viewport input")
	}
	screen.SetFocus(&scriptedComponent{})
	if screen.shouldDeferViewportInputToOverlayLocked() {
		t.Fatal("a plain focused component must not capture viewport input")
	}
}

type viewportCaptureHost struct{}

func (viewportCaptureHost) Rows() int                { return 24 }
func (viewportCaptureHost) RequestRender(force bool) {}

func TestEditorCapturesViewportInputOnlyWithAutocomplete(t *testing.T) {
	editor := NewEditor(viewportCaptureHost{}, EditorTheme{}, EditorOptions{})
	if editor.CapturesViewportInput() {
		t.Fatal("a closed autocomplete must not capture viewport input")
	}
	editor.autocompleteState = "regular"
	if !editor.CapturesViewportInput() {
		t.Fatal("an open autocomplete must capture viewport input")
	}
}
