package interactive

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/mcp"
	"github.com/dat267/pier/mcp/protocol"
)

// The MCP half of /reload in the composed app: CommandWiring.ReloadMCP
// exchanges the manager, the fresh wiring is installed, and the rebuilt tools
// land in the session registry (upstream session_start reason "reload" +
// _pendingToolNames).

func TestAppReloadMCPExchangesManager(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	writeMCPReloadConfig(t, app.mcpAgentDir, `{"mcpServers":{"first":{"command":"unused","exposure":"direct"}}}`)
	transport := func(entry coding.McpServerEntry, _ string, _ mcp.AuthProvider) (mcp.Transport, error) {
		server := newMCPReloadFakeServer(t, entry.Name, []protocol.Tool{{Name: "echo"}})
		return server, nil
	}
	app.mcpManager = coding.NewMcpManagerAsync(context.Background(), coding.McpManagerOptions{
		Config:          coding.LoadMcpConfig(coding.McpConfigLoadOptions{AgentDir: app.mcpAgentDir, Cwd: app.sessionMgr.GetCwd(), ProjectTrusted: true}),
		Cwd:             app.sessionMgr.GetCwd(),
		CreateTransport: transport,
	})
	app.wireMcpManager(app.mcpManager)
	if !app.mcpManager.WaitForDirectTools(context.Background(), 5*time.Second) {
		t.Fatal("startup connect did not settle")
	}
	// The rebuild callbacks post onto the renderer (the app loop drains them);
	// this test has no loop, so run the drain directly.
	app.ui.RenderNow(true)
	if app.session.GetToolDefinition("mcp__first__echo") == nil {
		t.Fatal("startup tools did not attach")
	}

	writeMCPReloadConfig(t, app.mcpAgentDir, `{"mcpServers":{"second":{"command":"unused","exposure":"direct"}}}`)
	manager, errors := app.mcpReloadForTest(transport)
	if manager == nil {
		t.Fatal("no rebuilt manager")
	}
	if len(errors) != 0 {
		t.Fatalf("reload errors = %v", errors)
	}
	if !manager.WaitForDirectTools(context.Background(), 5*time.Second) {
		t.Fatal("reload connect did not settle")
	}
	app.ui.RenderNow(true)
	if app.session.GetToolDefinition("mcp__second__echo") == nil {
		t.Fatal("reload tools did not attach through the fresh wiring")
	}
	if app.mcpManager != manager {
		t.Fatal("app still holds the old manager")
	}
}

// mcpReloadForTest mirrors CommandWiring.ReloadMCP's body with an injectable
// transport factory (the fake MCP server needs the registry seam).
func (a *App) mcpReloadForTest(transportFactory func(coding.McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error)) (*coding.McpManager, []string) {
	config := coding.LoadMcpConfig(coding.McpConfigLoadOptions{AgentDir: a.mcpAgentDir, Cwd: a.sessionMgr.GetCwd(), ProjectTrusted: true})
	var next *coding.McpManager
	if a.mcpManager == nil {
		next = coding.NewMcpManagerAsync(context.Background(), a.mcpOptionsForTest(config, transportFactory))
	} else {
		next = coding.ReloadMCPExchange(context.Background(), a.mcpManager, func(mcpCtx context.Context, reloaded coding.LoadedMcpConfig) *coding.McpManager {
			return coding.NewMcpManagerAsync(mcpCtx, a.mcpOptionsForTest(reloaded, transportFactory))
		}, config, nil)
	}
	a.mcpManager = next
	if next != nil {
		a.wireMcpManager(next)
	}
	return next, config.Errors
}

func (a *App) mcpOptionsForTest(config coding.LoadedMcpConfig, transportFactory func(coding.McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error)) coding.McpManagerOptions {
	return coding.McpManagerOptions{Config: config, Cwd: a.sessionMgr.GetCwd(), CreateTransport: transportFactory}
}

func loadMCPReloadCfg(t *testing.T, agentDir string) coding.LoadedMcpConfig {
	return coding.LoadMcpConfig(coding.McpConfigLoadOptions{AgentDir: agentDir, Cwd: agentDir, ProjectTrusted: true})
}

func writeMCPReloadConfig(t *testing.T, agentDir string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(agentDir, "mcp.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// directMcp is the direct exposure used by the reload fixture config.
var directMcp = coding.McpExposureDirect

// newMCPReloadFakeServer answers initialize and a fixed tools/list over an
// in-memory transport pair (the coding package's fake server fixture is
// test-package-private, so the smallest equivalent lives here).
func newMCPReloadFakeServer(t *testing.T, name string, tools []protocol.Tool) *mcp.InMemoryTransport {
	t.Helper()
	client, server := mcp.CreateInMemoryTransportPair()
	server.OnMessage(func(message mcp.Message) {
		request, ok := message.(*protocol.JsonRpcRequest)
		if !ok {
			return
		}
		go func() {
			var result any
			var failure *protocol.JsonRpcErrorObject
			switch request.Method {
			case "initialize":
				result = map[string]any{
					"protocolVersion": protocol.LatestProtocolVersion,
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": name, "version": "1.0.0"},
				}
			case "tools/list":
				entries := make([]any, 0, len(tools))
				for _, tool := range tools {
					entries = append(entries, map[string]any{
						"name": tool.Name, "description": "test " + tool.Name,
						"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
					})
				}
				result = map[string]any{"tools": entries}
			case "tools/call":
				result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}}
			default:
				failure = &protocol.JsonRpcErrorObject{Code: protocol.JSONRPCErrorCode.MethodNotFound, Message: "Method not found"}
			}
			response := protocol.JsonRpcResponse{JSONRPC: protocol.JSONRPCVersion, ID: request.ID}
			if failure != nil {
				response.Error = failure
			} else {
				encoded, err := json.Marshal(result)
				if err != nil {
					return
				}
				response.Result = encoded
			}
			_ = server.Send(context.Background(), response)
		}()
	})
	_ = server.Start(context.Background())
	t.Cleanup(func() { _ = client.Close(context.Background()); _ = server.Close(context.Background()) })
	return client
}
