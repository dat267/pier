package coding

import (
	"context"
	"testing"

	"github.com/dat267/pier/mcp"
)

// pi v1.4.0 (4a080252), extensions/mcp/runtime.ts getClient returns the shared
// open promise's result, not whatever client remains attached when a waiter
// resumes. A server can drop after publication but before open returns.
func TestMcpConnectRetainsItsResultAfterTransportDrop(t *testing.T) {
	server, transport := newMCPFakeServer(t, false)
	defer server.transport.Close(context.Background())
	connection := NewMcpServerConnection(McpServerConnectionOptions{
		Entry: McpServerEntry{Name: "fake", Config: &McpServerConfig{Command: "unused"}},
		CreateTransport: func(McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error) {
			return transport, nil
		},
		OnTools: func(*McpServerConnection) {
			// Drop the actual transport at this publication boundary; do not
			// mutate private connection fields to manufacture the interleaving.
			_ = server.transport.Close(context.Background())
		},
	})
	defer connection.Close(context.Background())
	client, err := connection.GetClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if client == nil {
		t.Fatal("a completed connect returned (nil, nil) after the transport dropped")
	}
	if client.ConnectionState() != mcp.StateClosed {
		t.Fatalf("dropped client state = %s, want closed", client.ConnectionState())
	}
}
