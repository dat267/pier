package tui

import (
	"testing"
	"testing/synctest"
	"time"
)

// pi v1.4.0 (4a080252), tui.ts queryTerminalColors settles timer/reply on
// the JS loop. Go's retained blocking OSC11 helper also waits off-loop, so
// its timeout and owner-loop reply must synchronize settlement bookkeeping.
func TestBackgroundQueryTimeoutReplyRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for range 20 {
			renderer := NewRenderer(&fakeTerminal{width: 80, height: 24})
			queryDone := make(chan struct{})
			replyDone := make(chan struct{})
			go func() {
				defer close(queryDone)
				color, ok := renderer.QueryTerminalBackgroundColor(1)
				if ok && color != (RgbColor{R: 10, G: 11, B: 12}) {
					t.Errorf("unexpected background: %+v", color)
				}
			}()
			synctest.Wait() // The query is waiting on its 1ms deadline.
			go func() {
				defer close(replyDone)
				time.Sleep(time.Millisecond)
				renderer.HandleTerminalInput("\x1b]11;#0a0b0c\x07")
			}()
			<-queryDone
			<-replyDone
		}
	})
}
