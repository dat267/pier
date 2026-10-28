package tui

import (
	"testing"
	"testing/synctest"
	"time"
)

// A listener registered before the blocking query still completes before the
// query's result is delivered, as it did in the original listener-based helper.
func TestColorSchemeQueryWaitsForPriorListeners(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		renderer := NewRenderer(&fakeTerminal{width: 80, height: 24})
		listenerStarted := make(chan struct{})
		releaseListener := make(chan struct{})
		defer close(releaseListener)
		unsubscribe := renderer.OnTerminalColorSchemeChange(func(TerminalColorScheme) {
			close(listenerStarted)
			<-releaseListener
		})
		defer unsubscribe()
		queryDone := make(chan struct{})
		go func() {
			renderer.QueryTerminalColorScheme(1000)
			close(queryDone)
		}()
		synctest.Wait()
		go renderer.HandleTerminalInput("\x1b[?997;1n")
		<-listenerStarted
		synctest.Wait()
		select {
		case <-queryDone:
			t.Fatal("query returned before the earlier listener finished")
		default:
		}
	})
}

// The retained blocking scheme helper must not register/unregister owner-loop
// listeners from its waiting goroutine. As with pi v1.4.0 tui.ts asynchronous
// queryTerminalColors, timeout/reply settlement must be serialized safely.
func TestColorSchemeQueryTimeoutReplyRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for range 20 {
			renderer := NewRenderer(&fakeTerminal{width: 80, height: 24})
			queryDone := make(chan struct{})
			replyDone := make(chan struct{})
			go func() {
				defer close(queryDone)
				scheme, ok := renderer.QueryTerminalColorScheme(1)
				if ok && scheme != TerminalColorSchemeDark {
					t.Errorf("unexpected color scheme: %s", scheme)
				}
			}()
			synctest.Wait()
			go func() {
				defer close(replyDone)
				time.Sleep(time.Millisecond)
				renderer.HandleTerminalInput("\x1b[?997;1n")
			}()
			<-queryDone
			<-replyDone
		}
	})
}
