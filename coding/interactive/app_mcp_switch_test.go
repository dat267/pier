package interactive

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/mcp"
	"github.com/dat267/pier/mcp/protocol"
)

// Reference: pi v1.4.0 src/core/agent-session-runtime.ts and
// src/extensions/mcp/index.ts (session_start and defaultLoadConfig).
//
// TestSessionReplacementReattachesMcpTools covers the MCP half of the
// session_start event upstream emits for every runtime bind: a resume or a new
// session re-reads mcp.json and reconnects, so tools connected before the
// replacement must be present on the incoming session too.
func TestSessionReplacementReattachesMcpTools(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	writeMCPReloadConfig(t, app.mcpAgentDir, `{"mcpServers":{"docs":{"command":"unused","exposure":"direct"}}}`)
	transport := func(entry coding.McpServerEntry, _ string, _ mcp.AuthProvider) (mcp.Transport, error) {
		return newMCPReloadFakeServer(t, entry.Name, []protocol.Tool{{Name: "echo"}}), nil
	}
	config := coding.LoadMcpConfig(coding.McpConfigLoadOptions{
		AgentDir: app.mcpAgentDir, Cwd: app.sessionMgr.GetCwd(), ProjectTrusted: true,
	})
	app.mcpOptionsOverride = func(loaded coding.LoadedMcpConfig) coding.McpManagerOptions {
		return coding.McpManagerOptions{Config: loaded, Cwd: app.sessionMgr.GetCwd(), CreateTransport: transport}
	}
	app.mcpManager = coding.NewMcpManagerAsync(context.Background(), coding.McpManagerOptions{
		Config: config, Cwd: app.sessionMgr.GetCwd(), CreateTransport: transport,
	})
	defer func() { _ = app.mcpManager.Close(context.Background()) }()
	app.wireMcpManager(app.mcpManager)
	if !app.mcpManager.WaitForDirectTools(context.Background(), 5*time.Second) {
		t.Fatal("startup connect did not settle")
	}
	app.ui.RenderNow(true)
	if app.session.GetToolDefinition("mcp__docs__echo") == nil {
		t.Fatal("startup tools did not attach")
	}

	// A server added while the app runs must arrive with the replacement: the
	// upstream session_start re-runs loadConfig, it does not just reattach the
	// old manager's tools.
	writeMCPReloadConfig(t, app.mcpAgentDir, `{"mcpServers":{"newdocs":{"command":"unused","exposure":"direct"}}}`)
	target := makePersistedSession(t, t.TempDir(), "from the other session")
	if _, err := app.switchSession(context.Background(), target.GetSessionFile(), ""); err != nil {
		t.Fatalf("switchSession: %v", err)
	}
	if app.mcpManager == nil || !app.mcpManager.WaitForDirectTools(context.Background(), 5*time.Second) {
		t.Fatal("replacement MCP connect did not settle")
	}
	app.ui.RenderNow(true)
	if app.session.GetToolDefinition("mcp__newdocs__echo") == nil {
		t.Fatal("the server added before the switch is missing from the resumed session")
	}
	if app.session.GetToolDefinition("mcp__docs__echo") != nil {
		t.Fatal("the replaced session kept tools from the old config")
	}
}

// TestReloadLoadsServerAddedAtRuntime covers the /reload half: a server added
// to mcp.json after startup arrives without restarting the app.
func TestReloadLoadsServerAddedAtRuntime(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	transport := func(entry coding.McpServerEntry, _ string, _ mcp.AuthProvider) (mcp.Transport, error) {
		return newMCPReloadFakeServer(t, entry.Name, []protocol.Tool{{Name: "echo"}}), nil
	}
	app.mcpOptionsOverride = func(loaded coding.LoadedMcpConfig) coding.McpManagerOptions {
		return coding.McpManagerOptions{Config: loaded, Cwd: app.sessionMgr.GetCwd(), CreateTransport: transport}
	}
	writeMCPReloadConfig(t, app.mcpAgentDir, `{"mcpServers":{"added":{"command":"unused","exposure":"direct"}}}`)
	manager, errors := app.commands.ReloadMCP(context.Background())
	if manager == nil || manager != app.mcpManager {
		t.Fatal("reload did not return the new manager")
	}
	defer func() { _ = manager.Close(context.Background()) }()
	if len(errors) != 0 {
		t.Fatalf("reload errors = %v", errors)
	}
	if !app.mcpManager.WaitForDirectTools(context.Background(), 5*time.Second) {
		t.Fatal("reload connect did not settle")
	}
	app.ui.RenderNow(true)
	if app.session.GetToolDefinition("mcp__added__echo") == nil {
		t.Fatal("the server added at runtime is missing after /reload")
	}
}

// TestReloadMCPHonorsProjectTrust covers the config read's trust gate:
// upstream defaultLoadConfig uses ctx.isProjectTrusted(), and the reload's
// implicit trust save runs after session_start.
func TestReloadMCPHonorsProjectTrust(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	transport := func(entry coding.McpServerEntry, _ string, _ mcp.AuthProvider) (mcp.Transport, error) {
		return newMCPReloadFakeServer(t, entry.Name, []protocol.Tool{{Name: "echo"}}), nil
	}
	app.mcpOptionsOverride = func(loaded coding.LoadedMcpConfig) coding.McpManagerOptions {
		return coding.McpManagerOptions{Config: loaded, Cwd: app.sessionMgr.GetCwd(), CreateTransport: transport}
	}
	app.settings.SetProjectTrusted(false)
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".pi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".pi", "mcp.json"),
		[]byte(`{"mcpServers":{"projectserver":{"command":"unused","exposure":"direct"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if errs := app.reloadMCP(context.Background(), project); len(errs) != 0 {
		t.Fatalf("untrusted reload errors = %v", errs)
	}
	if app.mcpManager != nil {
		t.Fatal("untrusted project MCP servers were loaded")
	}

	app.settings.SetProjectTrusted(true)
	if errs := app.reloadMCP(context.Background(), project); len(errs) != 0 {
		t.Fatalf("trusted reload errors = %v", errs)
	}
	if app.mcpManager == nil || !app.mcpManager.WaitForDirectTools(context.Background(), 5*time.Second) {
		t.Fatal("trusted project MCP connect did not settle")
	}
	defer func() { _ = app.mcpManager.Close(context.Background()) }()
	app.ui.RenderNow(true)
	if app.session.GetToolDefinition("mcp__projectserver__echo") == nil {
		t.Fatal("trusted project server is missing after reload")
	}
}
