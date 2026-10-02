package coding

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/mcp"
	"github.com/dat267/pier/mcp/protocol"
)

// TestMcpManagerExposesDirectTools covers the exposure split: only `direct`
// tools become agent tools; every server gets a namespaced, collision-free
// name.
func TestMcpManagerExposesDirectTools(t *testing.T) {
	direct := McpExposureDirect
	toolExposure := map[string]McpExposure{"search": McpExposureDirect, "hidden": McpExposureHidden}
	config := LoadedMcpConfig{Servers: []McpServerEntry{
		{Name: "docs", Scope: McpScopeGlobal, Config: &McpServerConfig{Command: "unused", Exposure: &direct}},
		{Name: "scripts", Scope: McpScopeGlobal, Config: &McpServerConfig{Command: "unused", ToolExposure: toolExposure}},
	}}
	manager := NewMcpManager(context.Background(), McpManagerOptions{
		Config: config,
		CreateTransport: func(entry McpServerEntry, _ string, _ mcp.AuthProvider) (mcp.Transport, error) {
			tools := []protocol.Tool{{Name: "echo"}, {Name: "search"}}
			if entry.Name == "scripts" {
				tools = []protocol.Tool{{Name: "search"}, {Name: "hidden"}, {Name: "codemode-only"}}
			}
			_, client := newMCPFakeServerTools(t, entry.Name, tools)
			return client, nil
		},
	})
	defer func() { _ = manager.Close(context.Background()) }()

	tools := manager.DirectTools()
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	// docs/echo and docs/search are direct; scripts/search is direct and
	// scripts/hidden is hidden; the codemode default exposes nothing.
	want := "mcp__docs__echo,mcp__docs__search,mcp__scripts__search"
	if got := strings.Join(names, ","); got != want {
		t.Fatalf("tools = %s, want %s", got, want)
	}
	if len(manager.Errors()) != 0 {
		t.Fatalf("errors = %v", manager.Errors())
	}
}

// TestMcpManagerReportsFailedServers covers a failed connection: the manager
// keeps the error and exposes no tools from it.
func TestMcpManagerReportsFailedServers(t *testing.T) {
	config := LoadedMcpConfig{Servers: []McpServerEntry{
		{Name: "broken", Scope: McpScopeGlobal, Config: &McpServerConfig{URL: "http://unused.invalid", Headers: map[string]string{"Authorization": "x"}}},
	}}
	attempts := 0
	manager := NewMcpManager(context.Background(), McpManagerOptions{
		Config: config,
		CreateTransport: func(McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error) {
			attempts++
			_, client := newMCPFakeServerTools(t, "broken", nil)
			return &failingTransport{
				Transport: client, method: "initialize",
				err: &mcp.McpHttpError{Status: 400, Msg: "MCP HTTP request failed with status 400: bad"},
			}, nil
		},
	})
	defer func() { _ = manager.Close(context.Background()) }()
	if len(manager.DirectTools()) != 0 {
		t.Fatalf("tools = %+v", manager.DirectTools())
	}
	errors := manager.Errors()
	if len(errors) != 1 || !strings.Contains(errors[0], "status 400: bad") {
		t.Fatalf("errors = %v", errors)
	}
	if connections := manager.Connections(); len(connections) != 1 || connections[0].State != McpServerFailed {
		t.Fatalf("connections = %+v", connections)
	}
}

// TestMcpManagerSkipsDisabledServers covers `enabled: false`.
func TestMcpManagerSkipsDisabledServers(t *testing.T) {
	disabled := false
	config := LoadedMcpConfig{Servers: []McpServerEntry{
		{Name: "off", Scope: McpScopeGlobal, Config: &McpServerConfig{Command: "unused", Enabled: &disabled}},
	}}
	manager := NewMcpManager(context.Background(), McpManagerOptions{Config: config})
	if len(manager.Connections()) != 0 {
		t.Fatalf("connections = %+v", manager.Connections())
	}
}

// newMCPFakeServerTools is newMCPFakeServer with a chosen tools/list answer.
func newMCPFakeServerTools(t *testing.T, name string, tools []protocol.Tool) (*mcpFakeServer, *mcp.InMemoryTransport) {
	t.Helper()
	client, server := mcp.CreateInMemoryTransportPair()
	fake := &mcpFakeServer{}
	server.OnMessage(func(message mcp.Message) {
		request, ok := message.(*protocol.JsonRpcRequest)
		if !ok {
			return
		}
		go func() {
			var result any
			var failure *protocol.McpError
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
					entries = append(entries, map[string]any{"name": tool.Name, "inputSchema": map[string]any{"type": "object"}})
				}
				result = map[string]any{"tools": entries}
			case "tools/call":
				result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}}
			default:
				failure = &protocol.McpError{Code: protocol.JSONRPCErrorCode.MethodNotFound, Msg: "Method not found"}
			}
			if failure != nil {
				_ = server.Send(context.Background(), protocol.JsonRpcResponse{
					JSONRPC: protocol.JSONRPCVersion, ID: request.ID,
					Error: &protocol.JsonRpcErrorObject{Code: failure.Code, Message: failure.Msg},
				})
				return
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				return
			}
			_ = server.Send(context.Background(), protocol.JsonRpcResponse{
				JSONRPC: protocol.JSONRPCVersion, ID: request.ID, Result: encoded,
			})
		}()
	})
	_ = server.Start(context.Background())
	return fake, client
}
