package coding

import (
	"context"
	"testing"
	"time"

	"github.com/dat267/pier/mcp"
	"github.com/dat267/pier/mcp/protocol"
)

// A direct server that connected and then dropped must not stall the first
// prompt again. pi v1.5.0 (f9a26365) extensions/mcp/index.ts keeps a `ready`
// promise per server that settles when that attempt connected or failed
// ("`server.ready` settles when it connected or failed"), and
// waitForDirectServers awaits those promises. A connection that drops later
// leaves its promise settled: handleClientClose only updates state and error,
// and reconnect/close are the only paths that replace `ready`. Polling the live
// state instead treats the drop as still-pending and waits the whole
// DEFAULT_STARTUP_WAIT_MS (10s), so the first prompt stalls for a server that
// already answered.
func TestWaitForDirectToolsReturnsAfterAConnectedServerDrops(t *testing.T) {
	direct := McpExposureDirect
	config := LoadedMcpConfig{Servers: []McpServerEntry{
		{Name: "docs", Scope: McpScopeGlobal, Config: &McpServerConfig{Command: "unused", Exposure: &direct}},
	}}
	var transport mcp.Transport
	manager := NewMcpManagerAsync(context.Background(), McpManagerOptions{
		Config: config,
		CreateTransport: func(entry McpServerEntry, _ string, _ mcp.AuthProvider) (mcp.Transport, error) {
			_, client := newMCPFakeServerTools(t, entry.Name, []protocol.Tool{{Name: "echo"}})
			transport = client
			return client, nil
		},
	})
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	if !manager.WaitForDirectTools(context.Background(), 10*time.Second) {
		t.Fatal("the first wait did not settle on a working server")
	}
	// The server drops after its tools were published.
	if err := transport.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	connection := manager.Connections()[0]
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		connection.mu.Lock()
		state := connection.State
		connection.mu.Unlock()
		if state == McpServerDisconnected {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	connection.mu.Lock()
	state := connection.State
	connection.mu.Unlock()
	if state != McpServerDisconnected {
		t.Fatalf("state = %s; want %s", state, McpServerDisconnected)
	}
	// The settled attempt is not pending again: this must return at once
	// rather than run out a short timeout.
	start := time.Now()
	if !manager.WaitForDirectTools(context.Background(), time.Second) {
		t.Fatalf("wait after the drop reported pending after %v; the settled attempt must not be awaited again", time.Since(start))
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("wait after the drop took %v; a settled attempt must not be awaited again", elapsed)
	}
}
