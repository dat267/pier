package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

// pi v1.4.0 client.ts arms request timers with setTimeout for every finite,
// positive timeout, which clamps delays above 2^31-1 to 1ms.
func TestClientLargeRequestTimeoutUsesJavaScriptTimerClamp(t *testing.T) {
	for _, test := range []struct {
		name      string
		timeoutMs int64
	}{
		{name: "over_timer_range", timeoutMs: 1 << 31},
		{name: "over_safe_integer", timeoutMs: 1 << 54},
	} {
		t.Run(test.name, func(t *testing.T) {
			timeoutMs := test.timeoutMs
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
			client := NewClient(ClientOptions{Name: "test-client", Version: "1.0.0"})
			if _, err := client.Connect(context.Background(), clientTransport); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				close(release)
				_ = client.Close(context.Background())
			})

			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			_, err := client.CallTool(ctx, "wait", nil, RequestOptions{TimeoutMs: timeoutMs})
			cancel()
			if _, ok := errors.AsType[*protocol.McpTimeoutError](err); !ok {
				t.Fatalf("timeout %d returned %v (%T), want MCP timeout", timeoutMs, err, err)
			}
			if err := client.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
