package tui

import (
	"testing"
	"time"
)

// pi v1.4.0 packages/tui/src/tui.ts completeTerminalColorQuery calls the late
// reply callback after retiring its delivery. Like any JS callback it can start
// another query synchronously; the port must not hold colorMu while calling it.
func TestTerminalColorLateReplyCanStartAnotherQuery(t *testing.T) {
	terminal := &fakeTerminal{width: 80, height: 24}
	renderer := NewRenderer(terminal)
	wait, ok := renderer.BeginTerminalColors()
	if !ok {
		t.Fatal("initial color query was not admitted")
	}
	started := make(chan bool, 1)
	wait(0, func(TerminalColors) {
		_, ok := renderer.BeginTerminalColors()
		started <- ok
	})
	dispatched := make(chan struct{})
	go func() {
		renderer.HandleTerminalInput("\x1b[?62;22c")
		close(dispatched)
	}()
	select {
	case <-dispatched:
	case <-time.After(time.Second):
		t.Fatal("late color callback deadlocked while starting another query")
	}
	if !<-started {
		t.Fatal("late callback could not start another query")
	}
	// The callback's query remains live and must receive the next DA1 reply.
	renderer.HandleTerminalInput("\x1b[?62;22c")
}
