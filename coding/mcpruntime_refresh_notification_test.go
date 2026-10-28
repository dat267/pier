package coding

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/dat267/pier/mcp"
	"github.com/dat267/pier/mcp/protocol"
)

// pi v1.4.0 (4a080252), extensions/mcp/runtime.ts starts refreshTools with
// `void`: awaiting tools/list inside the notification handler would prevent
// that same transport reader from delivering the response. Hold initial
// publication open so its context is still live, isolating reader reentrancy
// from the separate post-connect context-lifetime regression.
func TestMcpToolRefreshDoesNotBlockNotificationDelivery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, transport := newMCPFakeServer(t, false)
		defer server.transport.Close(context.Background())
		initial := make(chan struct{})
		releaseInitial := make(chan struct{})
		refreshed := make(chan struct{}, 1)
		var publications atomic.Int32
		connection := NewMcpServerConnection(McpServerConnectionOptions{
			Entry: McpServerEntry{Name: "fake", Config: &McpServerConfig{Command: "unused"}},
			CreateTransport: func(McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error) {
				return transport, nil
			},
			OnTools: func(*McpServerConnection) {
				if publications.Add(1) == 1 {
					close(initial)
					<-releaseInitial
				} else {
					refreshed <- struct{}{}
				}
			},
		})
		defer connection.Close(context.Background())
		defer close(releaseInitial)
		go func() { _, _ = connection.GetClient(context.Background()) }()
		<-initial
		if err := server.transport.Send(context.Background(), &protocol.JsonRpcNotification{
			JSONRPC: protocol.JSONRPCVersion,
			Method:  "notifications/tools/list_changed",
		}); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		select {
		case <-refreshed:
		default:
			t.Fatal("tool refresh blocked the reader that must deliver its response")
		}
	})
}
