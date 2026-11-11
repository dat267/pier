package coding

import (
	"context"
	"fmt"
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
