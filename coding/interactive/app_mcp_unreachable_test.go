package interactive

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/mcp"
	"github.com/dat267/pier/mcp/protocol"
)

// D185: /reload and session replacement re-read mcp.json (upstream
// session_start runs ensureDiscoveryActive again), so the warning for tools
// pier cannot expose comes back with the reload's MCP messages, after the
// config's own load errors, and only while such a server is configured.
func TestReloadMCPWarnsAboutUnreachableTools(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	transport := func(entry coding.McpServerEntry, _ string, _ mcp.AuthProvider) (mcp.Transport, error) {
		return newMCPReloadFakeServer(t, entry.Name, []protocol.Tool{{Name: "echo"}}), nil
	}
	app.mcpOptionsOverride = func(loaded coding.LoadedMcpConfig) coding.McpManagerOptions {
		return coding.McpManagerOptions{Config: loaded, Cwd: app.sessionMgr.GetCwd(), CreateTransport: transport}
	}
	defer func() {
		if app.mcpManager != nil {
			_ = app.mcpManager.Close(context.Background())
		}
	}()

	writeMCPReloadConfig(t, app.mcpAgentDir, `{"mcpServers":{"gopls":{"command":"unused"}}}`)
	messages := app.reloadMCP(context.Background(), app.sessionMgr.GetCwd())
	if !slices.ContainsFunc(messages, func(message string) bool {
		return strings.HasPrefix(message, `MCP server "gopls" has tools with codemode`)
	}) {
		t.Fatalf("reload messages = %q, want the unreachable-tools warning", messages)
	}

	writeMCPReloadConfig(t, app.mcpAgentDir, `{"mcpServers":{"gopls":{"command":"unused","exposure":"direct"}}}`)
	if messages := app.reloadMCP(context.Background(), app.sessionMgr.GetCwd()); len(messages) != 0 {
		t.Fatalf("direct-exposure reload messages = %q, want none", messages)
	}
}
