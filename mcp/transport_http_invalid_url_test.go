package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/dat267/pier/mcp/protocol"
)

// pi v1.4.0 (4a080252), transports/streamable-http.ts constructs
// `this.url = new URL(options.url)` in the constructor, so a malformed URL is a
// caller-visible error before any request. Start is the constructor's
// validating half in Go; a malformed URL must never reach a nil dereference.
func TestStreamableHTTPRejectsMalformedURLAtStart(t *testing.T) {
	transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: "http://[::1"})
	err := transport.Start(context.Background())
	if err == nil {
		t.Fatal("Start accepted a malformed URL")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("Start reported a cancellation for a malformed URL: %v", err)
	}
	sendErr := transport.Send(context.Background(), &protocol.JsonRpcNotification{JSONRPC: protocol.JSONRPCVersion, Method: "notifications/initialized"})
	if sendErr == nil {
		t.Fatal("Send after a rejected Start succeeded")
	}
}
