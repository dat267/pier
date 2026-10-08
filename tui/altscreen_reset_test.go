package tui

import "testing"

// A host that replaces the transcript resets the selection first, because the selection
// coordinates point into the transcript that is going away (upstream #9311).
func TestAltScreenResetTextSelection(t *testing.T) {
	terminal := &recordingTerminal{width: 20, height: 6}
	screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{})
	screen.DisableAutoRender()
	screen.AddChild(&scriptedComponent{lines: []string{"hello world"}})
	screen.Start()
	screen.RenderNow(false)

	press := func(data string) { screen.HandleTerminalInput(data) }
	press("\x1b[<0;1;1M")
	press("\x1b[<32;6;1M")
	press("\x1b[<0;6;1m")
	if !screen.HasActiveSelection() {
		t.Fatal("no active selection to reset")
	}
	// A press on a word records the multi-click history the next double click reads; it is
	// only held until the release, so take the reading before releasing.
	press("\x1b[<0;1;1M")
	if screen.lastClick == nil {
		t.Fatal("the press left no multi-click history to reset")
	}

	screen.ResetTextSelection()

	if screen.HasActiveSelection() {
		t.Fatal("the selection survived the reset")
	}
	if screen.lastClick != nil {
		t.Fatal("the multi-click history survived the reset")
	}
	if screen.selectionAnchor != nil || screen.selectionFocus != nil {
		t.Fatal("a selection point survived the reset")
	}
	if screen.selectionGranularity != "character" {
		t.Fatalf("granularity = %q, want character", screen.selectionGranularity)
	}
}
