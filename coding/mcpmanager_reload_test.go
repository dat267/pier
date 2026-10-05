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
