package tui

import (
	"testing"
	"time"
)

type timeoutQueryWriter struct{}

func (timeoutQueryWriter) Write(string) {}

// pi v1.4.0 tui.ts uses setTimeout for terminal-query deadlines, which clamps
// delays above 2^31-1 to 1ms.
func TestTerminalQueryTimeoutUsesJavaScriptTimerClamp(t *testing.T) {
	q := &terminalQueries{}
	result := make(chan bool, 1)
	go func() {
		_, ok := q.QueryBackground(timeoutQueryWriter{}, 1<<31)
		result <- ok
	}()
	select {
	case ok := <-result:
		if ok {
			t.Fatal("query succeeded without a terminal response")
		}
	case <-time.After(500 * time.Millisecond):
		q.ConsumeInput("\x1b]11;#000000\x07")
		select {
		case <-result:
		case <-time.After(time.Second):
			t.Fatal("query remained blocked after receiving a cleanup response")
		}
		t.Fatal("oversized terminal-query timeout did not use the JavaScript timer range")
	}
}
