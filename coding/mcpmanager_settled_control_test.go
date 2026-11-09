package coding

import (
	"context"
	"testing"
	"time"

	"github.com/dat267/pier/mcp"
)

// Control for TestWaitForDirectToolsReturnsAfterAConnectedServerDrops: a direct
// server that has not settled is still awaited, so the fix does not turn the
// first-prompt wait into a no-op. Upstream's ready promise is pending while
// getClient() has not resolved (index.ts startConnection).
func TestWaitForDirectToolsStillWaitsForAnUnsettledServer(t *testing.T) {
	direct := McpExposureDirect
	config := LoadedMcpConfig{Servers: []McpServerEntry{
		{Name: "slow", Scope: McpScopeGlobal, Config: &McpServerConfig{Command: "unused", Exposure: &direct}},
	}}
	release := make(chan struct{})
	manager := NewMcpManagerAsync(context.Background(), McpManagerOptions{
		Config: config,
		CreateTransport: func(McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error) {
			<-release
			_, client := mcp.CreateInMemoryTransportPair()
			return client, nil
		},
	})
	t.Cleanup(func() {
		close(release)
		_ = manager.Close(context.Background())
	})
	start := time.Now()
	if manager.WaitForDirectTools(context.Background(), 100*time.Millisecond) {
		t.Fatalf("an unsettled direct server was reported settled after %v", time.Since(start))
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("waited only %v; an unsettled server must hold the wait", elapsed)
	}
}
