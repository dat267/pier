package tui

import (
	"testing"
	"time"
)

// pi v1.4.0 stdin-buffer.ts uses setTimeout for incomplete-sequence deadlines,
// which clamps delays above 2^31-1 to 1ms.
func TestStdinBufferTimeoutUsesJavaScriptTimerClamp(t *testing.T) {
	cases := []struct {
		name    string
		options StdinBufferOptions
		input   string
	}{
		{name: "sequence", options: StdinBufferOptions{Timeout: 1 << 31}, input: "\x1b["},
		{name: "escape", options: StdinBufferOptions{EscapeTimeout: 1 << 31}, input: "\x1b"},
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
				t.Fatalf("pending deadline is %s from start; want at most 2ms", remaining)
			}
		})
	}
}
