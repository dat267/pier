package coding

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/dat267/pier/mcp"
	"github.com/dat267/pier/mcp/protocol"
)

// pi v1.4.0 (4a080252), extensions/mcp/runtime.ts connectOnce registers
// refreshTools without an opening-request signal. A completed connect must not
// cancel later notifications/tools/list_changed discovery on its live client.
func TestMcpToolRefreshSurvivesInitialConnectCompletion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, transport := newMCPFakeServer(t, false)
		defer server.transport.Close(context.Background())
		published := make(chan struct{}, 2)
		connection := NewMcpServerConnection(McpServerConnectionOptions{
			Entry: McpServerEntry{Name: "fake", Config: &McpServerConfig{Command: "unused"}},
			CreateTransport: func(McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error) {
				return transport, nil
			},
			OnTools: func(*McpServerConnection) { published <- struct{}{} },
		})
		defer connection.Close(context.Background())
		if _, err := connection.GetClient(context.Background()); err != nil {
			t.Fatal(err)
		}
		<-published // Initial tool discovery has been published.
		if err := server.transport.Send(context.Background(), &protocol.JsonRpcNotification{
			JSONRPC: protocol.JSONRPCVersion,
			Method:  "notifications/tools/list_changed",
		}); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		select {
		case <-published:
		default:
			connection.mu.Lock()
			err := connection.Error
			connection.mu.Unlock()
			t.Fatalf("live connection did not publish refreshed tools: %s", err)
		}
	})
}
