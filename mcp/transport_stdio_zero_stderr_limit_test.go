package mcp

import (
	"strings"
	"testing"
)

// pi v1.4.0 stdio.ts uses ?? for maxStderrBytes; subarray(-0) retains the full
// buffer rather than applying the default 64 KiB cap.
func TestStdioTransportExplicitZeroStderrLimitRetainsAllBytes(t *testing.T) {
	transport := NewStdioTransport(StdioTransportOptions{MaxStderrBytes: 0, MaxStderrBytesSet: true})
	chunk := strings.Repeat("x", defaultMaxStderrBytes+1)
	transport.handleStderr([]byte(chunk))
	if got := len(transport.Stderr()); got != len(chunk) {
		t.Fatalf("stderr bytes = %d, want %d with explicit zero limit", got, len(chunk))
	}
}
