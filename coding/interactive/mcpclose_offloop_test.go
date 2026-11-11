package interactive

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/internal/offloop"
	"github.com/dat267/pier/mcp"
)

// blockingCloseTransport wraps a working in-memory server and blocks in Close
// until the test releases it, the way a stdio server that ignores stdin close
// and SIGTERM does for the transport's whole grace-and-kill sequence.
type blockingCloseTransport struct {
	*mcp.InMemoryTransport
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (t *blockingCloseTransport) Close(ctx context.Context) error {
	t.once.Do(func() { close(t.entered) })
	<-t.release
	return t.InMemoryTransport.Close(ctx)
}

// TestMCPReplacementDoesNotWaitForTheClose pins A492: retiring the manager a
// session replacement leaves behind is shutdown work nothing waits on, and its
// Close can block for seconds on a server that ignores stdin close and SIGTERM
// (measured 2.50 s). The rebind must not sit inside that.
func TestMCPReplacementDoesNotWaitForTheClose(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	writeMCPReloadConfig(t, app.mcpAgentDir, `{"mcpServers":{}}`)
	entered := make(chan struct{})
	release := make(chan struct{})
	transport := func(entry coding.McpServerEntry, _ string, _ mcp.AuthProvider) (mcp.Transport, error) {
		return &blockingCloseTransport{
			InMemoryTransport: newMCPReloadFakeServer(t, entry.Name, nil),
			entered:           entered,
			release:           release,
		}, nil
	}
	app.mcpOptionsOverride = func(loaded coding.LoadedMcpConfig) coding.McpManagerOptions {
		return coding.McpManagerOptions{Config: loaded, Cwd: app.sessionMgr.GetCwd(), CreateTransport: transport}
	}
	// Direct, so WaitForDirectTools waits for the connect and the transport is
	// installed before the replacement retires this manager. A lazy entry would
	// let Close run against a connection that has nothing to close yet.
	direct := coding.McpExposureDirect
	config := coding.LoadedMcpConfig{Servers: []coding.McpServerEntry{
		{Name: "hung", Scope: coding.McpScopeGlobal, Config: &coding.McpServerConfig{Command: "unused", Exposure: &direct}},
	}}
	app.mcpManager = coding.NewMcpManagerAsync(context.Background(), coding.McpManagerOptions{
		Config: config, Cwd: app.sessionMgr.GetCwd(), CreateTransport: transport,
	})
	if !app.mcpManager.WaitForDirectTools(context.Background(), 5*time.Second) {
		t.Fatal("the outgoing connection did not settle")
	}

	app.sessionQueue = offloop.New()
	defer app.sessionQueue.Stop()

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		app.reloadMCP(context.Background(), app.sessionMgr.GetCwd())
	}()
	// Generous: under `go test ./...` on four cores the worker can be queued
	// behind other packages. The bug this pins never delays this first step (the
	// old code entered Close inline), so the margin costs nothing.
	select {
	case <-entered:
	case <-time.After(60 * time.Second):
		t.Fatal("the retired manager was never closed")
	}
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		close(release)
		<-returned
		t.Fatal("the replacement waited for the close")
	}
	close(release)
	app.sessionQueue.Flush()
}
