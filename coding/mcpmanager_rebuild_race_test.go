package coding

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/mcp"
	"github.com/dat267/pier/mcp/protocol"
)

// Servers that connect at once each rebuild the direct tool set from what has
// connected so far. A rebuild that read the connections earlier must not land
// after a later one and drop the tools of a server only the later one saw.
// Upstream extensions/mcp/index.ts registers each server's tools as its
// connect settles, on one event loop (pi v1.5.2 b4afdddf); the port's rebuilds
// run on the connect goroutines.
func TestMcpConcurrentConnectsKeepEveryServersTools(t *testing.T) {
	direct := McpExposureDirect
	const servers = 8
	for round := range 20 {
		var entries []McpServerEntry
		for index := range servers {
			entries = append(entries, McpServerEntry{
				Name: fmt.Sprintf("s%d", index), Scope: McpScopeGlobal,
				Config: &McpServerConfig{Command: "unused", Exposure: &direct},
			})
		}
		manager := NewMcpManagerAsync(context.Background(), McpManagerOptions{
			Config: LoadedMcpConfig{Servers: entries},
			CreateTransport: func(entry McpServerEntry, _ string, _ mcp.AuthProvider) (mcp.Transport, error) {
				_, client := newMCPFakeServerTools(t, entry.Name, []protocol.Tool{{Name: "echo"}})
				return client, nil
			},
		})
		if !manager.WaitForDirectTools(context.Background(), 10*time.Second) {
			t.Fatal("the servers did not settle")
		}
		got := len(manager.DirectTools())
		_ = manager.Close(context.Background())
		if got != servers {
			t.Fatalf("round %d: %d direct tools after every server connected, want %d", round, got, servers)
		}
	}
}

// The same for the session: each rebuild's callback attaches the manager's
// tool set, and a callback that read the set before a later rebuild published
// must not attach after it and drop a server's tools from the session. Nor may
// two attaches that each add a tool to the active set drop the other's.
func TestMcpConcurrentConnectsAttachEveryServersToolsToTheSession(t *testing.T) {
	direct := McpExposureDirect
	const servers = 8
	for round := range 20 {
		var entries []McpServerEntry
		for index := range servers {
			entries = append(entries, McpServerEntry{
				Name: fmt.Sprintf("s%d", index), Scope: McpScopeGlobal,
				Config: &McpServerConfig{Command: "unused", Exposure: &direct},
			})
		}
		sessions, dir := newTestSession(t)
		session, err := NewAgentSession(&SessionConfig{Cwd: dir, Sessions: sessions, StreamFn: stubStreamFn})
		if err != nil {
			t.Fatal(err)
		}
		manager := NewMcpManagerAsync(context.Background(), McpManagerOptions{
			Config: LoadedMcpConfig{Servers: entries},
			CreateTransport: func(entry McpServerEntry, _ string, _ mcp.AuthProvider) (mcp.Transport, error) {
				_, client := newMCPFakeServerTools(t, entry.Name, []protocol.Tool{{Name: "echo"}})
				return client, nil
			},
		})
		manager.AttachSession(session)
		if !manager.WaitForDirectTools(context.Background(), 10*time.Second) {
			t.Fatal("the servers did not settle")
		}
		attached := 0
		for _, name := range session.GetActiveToolNames() {
			if strings.HasPrefix(name, "mcp__") {
				attached++
			}
		}
		_ = manager.Close(context.Background())
		session.Dispose()
		if attached != servers {
			t.Fatalf("round %d: %d MCP tools active in the session after every server connected, want %d", round, attached, servers)
		}
	}
}
