package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

// pi v1.4.0 client.ts uses nullish fallback for request timeoutMs, so an
// explicit zero overrides the client default and disables the request timer.
func TestClientExplicitZeroTimeoutOverridesClientDefault(t *testing.T) {
	server, clientTransport := newTestServer()
	server.setHandler("initialize", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{
			"protocolVersion": protocol.LatestProtocolVersion,
			"capabilities":    map[string]any{},
			"serverInfo":      map[string]any{"name": "test-server", "version": "1.0.0"},
		}, nil
	})
	release := make(chan struct{})
	server.setHandler("tools/call", func(*protocol.JsonRpcRequest) (any, error) {
		<-release
		return map[string]any{"content": []any{}}, nil
	})
	// Keep initialize's default timeout generous under the race detector; the
	// later caller deadline still distinguishes zero from this client default.
	client := NewClient(ClientOptions{Name: "test-client", Version: "1.0.0", RequestTimeoutMs: 100})
	if _, err := client.Connect(context.Background(), clientTransport); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		close(release)
		_ = client.Close(context.Background())
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := client.CallTool(ctx, "wait", nil, RequestOptions{TimeoutMs: 0, TimeoutMsSet: true})
	if _, ok := errors.AsType[*protocol.McpAbortError](err); !ok {
		t.Fatalf("explicit zero returned %v (%T), want caller-context abort", err, err)
	}
}
