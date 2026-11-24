package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
					entry := map[string]any{"name": tool.Name, "inputSchema": map[string]any{"type": "object"}}
					if tool.Description != nil {
						entry["description"] = *tool.Description
					}
					entries = append(entries, entry)
				}
				result = map[string]any{"tools": entries}
			case "tools/call":
				result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}}
			default:
				failure = &protocol.McpError{Code: protocol.JSONRPCErrorCode.MethodNotFound, Msg: "Method not found"}
			}
			replyJSONRPC(server, request.ID, result, failure)
		}()
	})
	_ = server.Start(context.Background())
	return fake, client
}

// pi v1.4.0 extensions/mcp/runtime.ts stores the failure before notifying
// observers. Go observers must snapshot state and its diagnostic together.
// This small bounded race probe uses public reconnect/error-query paths;
// `go test -race` must not report a diagnostic read outside the state lock.
func TestMcpConnectionErrorsSnapshotDuringReconnect(t *testing.T) {
	var attempts atomic.Int32
	manager := NewMcpManager(context.Background(), McpManagerOptions{
		Config: LoadedMcpConfig{Servers: []McpServerEntry{
			{Name: "fake", Config: &McpServerConfig{Command: "unused"}},
		}},
		CreateTransport: func(McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error) {
			return nil, fmt.Errorf("failure %d", attempts.Add(1))
		},
	})
	defer manager.Close(context.Background())
	connection := manager.Connections()[0]
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Go(func() {
		<-start
		for range 100 {
			if _, err := connection.GetClient(context.Background()); err == nil {
				t.Error("failed transport unexpectedly connected")
			}
		}
	})
	workers.Go(func() {
		<-start
		for range 1000 {
			if got := manager.ConnectionErrors(); len(got) > 1 {
				t.Errorf("one server returned %d errors", len(got))
			}
		}
	})
	close(start)
	workers.Wait()
}

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

// TestMcpToolNamesIgnoreToolListOrder pins that a name collision inside one
// server hashes both tools, not only the later one. Upstream computes the
// collisions first ([...new Set(connection.tools.map(t => t.name))].map(...))
// and treats a candidate as taken when `plain.indexOf(candidate) !==
// plain.lastIndexOf(candidate)`, so "which one would keep the plain name does
// not depend on the order of the list"
// (extensions/mcp/index.ts registerTools, pi v1.5.0 f9a26365).
func TestMcpToolNamesIgnoreToolListOrder(t *testing.T) {
	direct := McpExposureDirect
	namesFor := func(tools []protocol.Tool) []string {
		t.Helper()
		config := LoadedMcpConfig{Servers: []McpServerEntry{
			{Name: "srv", Scope: McpScopeGlobal, Config: &McpServerConfig{Command: "unused", Exposure: &direct}},
		}}
		manager := NewMcpManager(context.Background(), McpManagerOptions{
			Config: config,
			CreateTransport: func(entry McpServerEntry, _ string, _ mcp.AuthProvider) (mcp.Transport, error) {
				_, client := newMCPFakeServerTools(t, entry.Name, tools)
				return client, nil
			},
		})
		defer func() { _ = manager.Close(context.Background()) }()
		names := make([]string, 0, len(tools))
		for _, tool := range manager.DirectTools() {
			names = append(names, tool.Name)
		}
		return names
	}

	// "a.b" and "a_b" sanitize to the same provider name.
	forward := namesFor([]protocol.Tool{{Name: "a.b"}, {Name: "a_b"}})
	reversed := namesFor([]protocol.Tool{{Name: "a_b"}, {Name: "a.b"}})
	if len(forward) != 2 || len(reversed) != 2 {
		t.Fatalf("tools = %v / %v", forward, reversed)
	}
	for _, name := range forward {
		if !strings.HasSuffix(name, "_") && !isHashedMcpToolName(name) {
			t.Errorf("name %q kept the plain name; both colliding tools must be hashed", name)
		}
	}
	if forward[0] != reversed[1] || forward[1] != reversed[0] {
		t.Errorf("names depend on order: forward %v, reversed %v", forward, reversed)
	}
}

// isHashedMcpToolName reports the `_<8 hex>` suffix createMcpToolName appends.
func isHashedMcpToolName(name string) bool {
	if len(name) < 9 || name[len(name)-9] != '_' {
		return false
	}
	for _, r := range name[len(name)-8:] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// replyJSONRPC answers request id on a fake server's transport: an error
// response when failure is set, otherwise result encoded. A result that does
// not encode sends nothing.
func replyJSONRPC(server mcp.Transport, id protocol.JsonRpcId, result any, failure *protocol.McpError) {
	response := protocol.JsonRpcResponse{JSONRPC: protocol.JSONRPCVersion, ID: id}
	if failure != nil {
		response.Error = &protocol.JsonRpcErrorObject{Code: failure.Code, Message: failure.Msg}
	} else {
		encoded, err := json.Marshal(result)
		if err != nil {
			return
		}
		response.Result = encoded
	}
	_ = server.Send(context.Background(), response)
}
