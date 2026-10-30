package tui

import (
	"testing"
	"time"
)

// pi v1.4.0 stdin-buffer.ts uses ?? for both timeouts; explicit zero reaches
// setTimeout and clamps to 1ms instead of selecting the defaults.
func TestStdinBufferExplicitZeroTimeoutUsesTimerMinimum(t *testing.T) {
	cases := []struct {
		name    string
		options StdinBufferOptions
		input   string
	}{
		{name: "sequence", options: StdinBufferOptions{Timeout: 0, TimeoutSet: true}, input: "\x1b["},
		{name: "escape", options: StdinBufferOptions{EscapeTimeout: 0, EscapeTimeoutSet: true}, input: "\x1b"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			buffer := NewStdinBuffer(test.options)
			started := time.Now()
			buffer.ProcessString(test.input)
			deadline, ok := buffer.PendingTimeout()
			if !ok {
				t.Fatal("incomplete sequence has no pending timeout")
			}
			if remaining := deadline.Sub(started); remaining <= 0 || remaining > 2*time.Millisecond {
				t.Fatalf("pending deadline is %s from start; want 1ms timer minimum", remaining)
			}
		})
	}
}
