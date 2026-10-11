package coding

import (
	"context"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/mcp"
	"github.com/dat267/pier/mcp/protocol"
)

// The /reload MCP half (upstream reload(): the extension's session_start
// re-runs loadConfig and reconnects every enabled server, then the tools
// activate into the live registry).

// registryToolNames lists the session's registered tool names.
func (s *AgentSession) registryToolNames() []string {
	s.control.stateMu.Lock()
	defer s.control.stateMu.Unlock()
	names := make([]string, 0, len(s.control.Tools))
	for name := range s.control.Tools {
		names = append(names, name)
	}
	return names
}

func testAgentSessionForReload(t *testing.T) *AgentSession {
	t.Helper()
	dir := t.TempDir()
	sessions := NewSessionManager(dir, &SessionManagerOptions{SessionDir: dir})
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Model: &ai.Model{ID: "mock", API: "openai-responses", Provider: "openai", ContextWindow: 200000},
		StreamFn: mockSessionStreamFn(createAssistantMessageT("reply")),
		Sessions: sessions,
	})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

// TestReloadMCPRefreshesSameNamedTool follows core/agent-session.ts's registry
// refresh: replacing a registered tool updates it, and withdrawn tools disappear.
func TestReloadMCPRefreshesSameNamedTool(t *testing.T) {
	session := testAgentSessionForReload(t)
	direct := McpExposureDirect
	config := LoadedMcpConfig{Servers: []McpServerEntry{
		{Name: "docs", Scope: McpScopeGlobal, Config: &McpServerConfig{Command: "unused", Exposure: &direct}},
	}}
	description := "before reload"
	includeTool := true
	transport := func(entry McpServerEntry, _ string, _ mcp.AuthProvider) (mcp.Transport, error) {
		var tools []protocol.Tool
		if includeTool {
			toolDescription := description
			tools = []protocol.Tool{{Name: "echo", Description: &toolDescription}}
		}
		_, client := newMCPFakeServerTools(t, entry.Name, tools)
		return client, nil
	}
	manager := NewMcpManagerAsync(context.Background(), McpManagerOptions{
		Config: config, CreateTransport: transport,
	})
	defer func() { _ = manager.Close(context.Background()) }()
	if !manager.WaitForDirectTools(context.Background(), 5e9) {
		t.Fatal("initial connect timed out")
	}
	manager.AttachSession(session)

	description = "after reload"
	reloaded := ReloadMCPExchange(context.Background(), manager, nil, config, nil)
	if reloaded == nil {
		t.Fatal("no rebuilt manager")
	}
	defer func() { _ = reloaded.Close(context.Background()) }()
	reloaded.AttachSession(session)
	if !reloaded.WaitForDirectTools(context.Background(), 5e9) {
		t.Fatal("reload connect timed out")
	}

	found := false
	for _, tool := range session.State().Tools {
		if tool.Name == "mcp__docs__echo" {
			found = true
			if tool.Description != description {
				t.Fatalf("active MCP tool description = %q, want %q", tool.Description, description)
			}
		}
	}
	if !found {
		t.Fatal("reloaded MCP tool is not active")
	}

	// The next snapshot no longer offers the tool. It must disappear from both
	// the model's active set and the session registry.
	includeTool = false
	withoutTool := ReloadMCPExchange(context.Background(), reloaded, nil, config, nil)
	if withoutTool == nil {
		t.Fatal("no manager after tool removal")
	}
	defer func() { _ = withoutTool.Close(context.Background()) }()
	withoutTool.AttachSession(session)
	if !withoutTool.WaitForDirectTools(context.Background(), 5e9) {
		t.Fatal("tool-removal connect timed out")
	}
	for _, tool := range session.State().Tools {
		if tool.Name == "mcp__docs__echo" {
			t.Fatal("removed MCP tool remains active")
		}
	}
	if session.GetToolDefinition("mcp__docs__echo") != nil {
		t.Fatal("removed MCP tool remains registered")
	}
}

// TestReloadMCPCloseAndRebuild covers the manager swap: ReloadMCPClose closes
// every connection of the old manager and ReloadMCPRebuild starts a fresh set
// through the factory; tools arriving after the swap attach to the session.
func TestReloadMCPCloseAndRebuild(t *testing.T) {
	session := testAgentSessionForReload(t)
	direct := McpExposureDirect
	newConfig := func(name string) LoadedMcpConfig {
		return LoadedMcpConfig{Servers: []McpServerEntry{
			{Name: name, Scope: McpScopeGlobal, Config: &McpServerConfig{Command: "unused", Exposure: &direct}},
		}}
	}
	first := NewMcpManagerAsync(context.Background(), McpManagerOptions{
		Config: newConfig("old"),
		CreateTransport: func(entry McpServerEntry, _ string, _ mcp.AuthProvider) (mcp.Transport, error) {
			_, client := newMCPFakeServerTools(t, entry.Name, []protocol.Tool{{Name: "echo"}})
			return client, nil
		},
	})
	defer func() { _ = first.Close(context.Background()) }()
	first.AttachSession(session)

	reloaded := ReloadMCPExchange(context.Background(), first, nil, newConfig("new"),
		func(entry McpServerEntry, _ string, _ mcp.AuthProvider) (mcp.Transport, error) {
			_, client := newMCPFakeServerTools(t, entry.Name, []protocol.Tool{{Name: "echo"}})
			return client, nil
		})
	if reloaded == nil {
		t.Fatal("no rebuilt manager")
	}
	defer func() { _ = reloaded.Close(context.Background()) }()
	reloaded.AttachSession(session)
	if !reloaded.WaitForDirectTools(context.Background(), 5e9) {
		t.Fatal("rebuild wait timed out")
	}
	// AttachSession's rebuild callback already activated the new tools into
	// the session when the connect settled, so the explicit attach is a no-op;
	// the registry entry is the assertion (upstream _pendingToolNames activates
	// the reconnected tools whenever they land).
	if session.GetToolDefinition("mcp__new__echo") == nil {
		t.Fatalf("registry missing the reloaded tool; registry names = %v", session.registryToolNames())
	}
}
