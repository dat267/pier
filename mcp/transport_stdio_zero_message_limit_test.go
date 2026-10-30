package mcp

import (
	"strings"
	"testing"
)

// pi v1.4.0 stdio.ts uses ?? for maxMessageBytes, so an explicit zero rejects
// every nonempty frame instead of selecting the default limit.
func TestStdioTransportExplicitZeroMessageLimit(t *testing.T) {
	transport := NewStdioTransport(StdioTransportOptions{MaxMessageBytes: 0, MaxMessageBytesSet: true})
	var messages []Message
	var errors []error
	transport.OnMessage(func(message Message) { messages = append(messages, message) })
	transport.OnError(func(err error) { errors = append(errors, err) })

	transport.handleStdout([]byte("{\"jsonrpc\":\"2.0\",\"method\":\"ping\"}\n"))
	if len(messages) != 0 {
		t.Fatalf("decoded %d messages despite a zero-byte limit", len(messages))
	}
	if len(errors) != 1 || !strings.Contains(errors[0].Error(), "exceeds 0 bytes") {
		t.Fatalf("errors = %v, want one zero-limit error", errors)
	}
}
