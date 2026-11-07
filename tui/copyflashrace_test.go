package tui

import (
	"strings"
	"testing"
	"time"
)

// Reference: pi v1.5.0 (f9a26365) packages/tui/src/tui-alt-screen.ts
// copyTextToClipboard. Upstream's copySelection continuation is an async
// function, so its follow-up flash resumes on the single-threaded event loop
// and cannot race the render clock. The port runs the continuation on a
// goroutine (the CopySelectionFn hint cannot express completion), so the
// follow-up Flash has to marshal through Renderer.Post (postMu) instead of
// mutating the flash container from the goroutine. A418: without that, a copy
// released while the owner flashes or renders races
// AltScreenFlashContainer.entries.
func TestReleaseCopyFlashMarshalsThroughPost(t *testing.T) {
	terminal := &recordingTerminal{width: 30, height: 8}
	startCopy := make(chan struct{}, 1)
	release := make(chan struct{})
	screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{
		CopySelection: func(text string) (bool, bool, string) {
			startCopy <- struct{}{}
			<-release
			return true, true, ""
		},
	})
	screen.DisableAutoRender()
	screen.AddChild(&scriptedComponent{lines: []string{"hello world"}})
	screen.Start()
	defer screen.Stop(TuiStopOptions{})

	input := func(data string) { screen.HandleTerminalInput(data) }
	screen.RenderNow(false)
	input("\x1b[<0;1;1M")
	input("\x1b[<32;6;1M")
	// The release runs the copy on a goroutine; the fixture blocks it there.
	input("\x1b[<0;6;1m")
	select {
	case <-startCopy:
		// The callback owns this goroutine while the owner loop flashes a frame.
	case <-time.After(2 * time.Second):
		t.Fatal("the copy callback did not start")
	}
	// The owner keeps flashing while the copy goroutine holds one.
	for index := 0; index < 200; index++ {
		screen.Flash("main")
		time.Sleep(time.Millisecond)
	}
	close(release)
	time.Sleep(200 * time.Millisecond)
	// A final frame drains the posted flash and paints it.
	terminal.writes = nil
	screen.RenderNow(false)
	writes := strings.Join(terminal.writes, "\x00")
	if !strings.Contains(writes, "Copied!") {
		t.Fatalf("the copy's flash never reached a frame:\n%s", writes)
	}
}
