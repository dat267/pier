package coding

import (
	"context"
	"testing"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/mcp"
	"github.com/dat267/pier/mcp/protocol"
)

// Tests for the asynchronous MCP connect (upstream extensions/mcp: connections
// start at boot without blocking it, the first prompt waits for servers whose
// tools are declared to the model, capped, and late tools attach live).

func TestHasMcpDirectTools(t *testing.T) {
	direct := McpExposureDirect
	codemode := McpExposureCodemode
	entries := []struct {
		name  string
		entry McpServerEntry
		want  bool
	}{
		{"default is codemode", McpServerEntry{Name: "a", Config: &McpServerConfig{Command: "x"}}, false},
		{"server exposure direct", McpServerEntry{Name: "b", Config: &McpServerConfig{Command: "x", Exposure: &direct}}, true},
		{"server exposure codemode", McpServerEntry{Name: "c", Config: &McpServerConfig{Command: "x", Exposure: &codemode}}, false},
		{"per-tool direct", McpServerEntry{Name: "d", Config: &McpServerConfig{Command: "x", ToolExposure: map[string]McpExposure{"search": direct}}}, true},
		{"star pattern direct", McpServerEntry{Name: "e", Config: &McpServerConfig{Command: "x", ToolExposure: map[string]McpExposure{"*": direct}}}, true},
		{"nil config", McpServerEntry{Name: "f"}, false},
	}
	for _, tc := range entries {
		if got := HasMcpDirectTools(tc.entry); got != tc.want {
			t.Errorf("%s: HasMcpDirectTools = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestNewMcpManagerAsyncConnectsInTheBackground: the constructor returns
// before the connections settle, and WaitForDirectTools waits them out.
func TestNewMcpManagerAsyncConnectsInTheBackground(t *testing.T) {
	direct := McpExposureDirect
	config := LoadedMcpConfig{Servers: []McpServerEntry{
		{Name: "docs", Scope: McpScopeGlobal, Config: &McpServerConfig{Command: "unused", Exposure: &direct}},
	}}
	started := make(chan struct{})
	manager := NewMcpManagerAsync(context.Background(), McpManagerOptions{
		Config: config,
		CreateTransport: func(entry McpServerEntry, _ string, _ mcp.AuthProvider) (mcp.Transport, error) {
			select {
			case <-started:
			default:
				close(started)
			}
			_, client := newMCPFakeServerTools(t, entry.Name, []protocol.Tool{{Name: "echo"}})
			return client, nil
		},
	})
	defer func() { _ = manager.Close(context.Background()) }()

	// The constructor returns while the connect is still running at least
	// sometimes; with a direct server the wait must settle true either way.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("connect did not start in the background")
	}
	if !manager.WaitForDirectTools(context.Background(), 10*time.Second) {
		t.Fatal("WaitForDirectTools timed out on a working server")
	}
	if len(manager.DirectTools()) == 0 {
		t.Fatal("no direct tools after the connect settled")
	}
}

// TestWaitForDirectToolsTimesOut: a hanging direct server is given up on after
// the timeout (upstream DEFAULT_STARTUP_WAIT_MS), and the wait is false.
func TestWaitForDirectToolsTimesOut(t *testing.T) {
	direct := McpExposureDirect
	config := LoadedMcpConfig{Servers: []McpServerEntry{
		{Name: "slow", Scope: McpScopeGlobal, Config: &McpServerConfig{Command: "unused", Exposure: &direct}},
	}}
	release := make(chan struct{})
	manager := NewMcpManagerAsync(context.Background(), McpManagerOptions{
		Config: config,
		CreateTransport: func(McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error) {
			<-release
			return nil, context.Canceled
		},
	})
	defer func() {
		close(release)
		_ = manager.Close(context.Background())
	}()
	if manager.WaitForDirectTools(context.Background(), 50*time.Millisecond) {
		t.Fatal("wait reported settled while the transport hung")
	}
}

// TestWaitForDirectToolsSkipsCodemode: servers without declared tools are not
// waited for (upstream waits only for hasDirectTools servers).
func TestWaitForDirectToolsSkipsCodemode(t *testing.T) {
	config := LoadedMcpConfig{Servers: []McpServerEntry{
		{Name: "codemode-only", Scope: McpScopeGlobal, Config: &McpServerConfig{Command: "unused"}},
	}}
	manager := NewMcpManagerAsync(context.Background(), McpManagerOptions{
		Config: config,
		CreateTransport: func(McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error) {
			return nil, context.Canceled
		},
	})
	defer func() { _ = manager.Close(context.Background()) }()
	if !manager.WaitForDirectTools(context.Background(), 50*time.Millisecond) {
		t.Fatal("waited for a codemode-only server")
	}
}

// TestAttachExtraTools: late MCP tools register into the live session, become
// active unless excluded, and a repeat attach is a no-op.
func TestAttachExtraTools(t *testing.T) {
	dir := t.TempDir()
	sessions := NewSessionManager(dir, &SessionManagerOptions{SessionDir: dir})
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Model: &ai.Model{ID: "mock", API: "openai-responses", Provider: "openai", ContextWindow: 200000}, StreamFn: mockSessionStreamFn(createAssistantMessageT("reply")),
		Sessions: sessions,
	})
	if err != nil {
		t.Fatal(err)
	}
	before := len(session.ActiveToolNames())
	tool := agent.AgentTool{Name: "mcp__docs__search", Description: "search"}
	added := session.AttachExtraTools([]agent.AgentTool{tool})
	if len(added) != 1 || added[0] != "mcp__docs__search" {
		t.Fatalf("added = %v", added)
	}
	if len(session.ActiveToolNames()) != before+1 {
		t.Fatalf("active = %v", session.ActiveToolNames())
	}
	if session.GetToolDefinition("mcp__docs__search") == nil {
		t.Fatal("definition missing after attach")
	}
	if again := session.AttachExtraTools([]agent.AgentTool{tool}); again != nil {
		t.Fatalf("second attach added %v", again)
	}
}

// TestBeforeFirstTurnRunsOnce: the hook fires on the first turn only.
func TestBeforeFirstTurnRunsOnce(t *testing.T) {
	dir := t.TempDir()
	sessions := NewSessionManager(dir, &SessionManagerOptions{SessionDir: dir})
	calls := 0
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Model: &ai.Model{ID: "mock", API: "openai-responses", Provider: "openai", ContextWindow: 200000}, StreamFn: mockSessionStreamFn(
			createAssistantMessageT("one"), createAssistantMessageT("two")),
		Sessions:        sessions,
		BeforeFirstTurn: func(context.Context) { calls++ },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.PromptText(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if err := session.PromptText(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("hook ran %d times, want 1", calls)
	}
}
