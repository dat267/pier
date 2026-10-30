package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

// pi v1.4.0 client.ts uses ?? for requestTimeoutMs, so an explicit client-level
// zero disables the inherited request timer instead of selecting 30 seconds.
func TestClientExplicitZeroDefaultTimeoutDisablesTimer(t *testing.T) {
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
	client := NewClient(ClientOptions{Name: "test-client", Version: "1.0.0", RequestTimeoutMs: 0, RequestTimeoutMsSet: true})
	if _, err := client.Connect(context.Background(), clientTransport); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		close(release)
		_ = client.Close(context.Background())
	})
	go func() { _, _ = client.CallTool(context.Background(), "wait", nil, RequestOptions{}) }()

	deadline := time.Now().Add(time.Second)
	for {
		client.mu.Lock()
		var timeoutMs int64
		for _, pending := range client.pending {
			timeoutMs = pending.timeoutMs
			break
		}
		pending := len(client.pending) > 0
		client.mu.Unlock()
		if pending {
			if timeoutMs != 0 {
				t.Fatalf("pending request timeout = %dms, want explicit zero", timeoutMs)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("request was never pending")
		}
		time.Sleep(time.Millisecond)
	}
}
