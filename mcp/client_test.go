package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dat267/pier/mcp/protocol"
	"strings"
	"testing"
	"time"
)

// Port of packages/mcp/test/client.test.ts: the in-memory pair plays the
// server, capturing every message and answering from per-method handlers.

func TestClientInitializesBeforeExposingServerInformation(t *testing.T) {
	client, server := connectTestClient(t)
	if client.ConnectionState() != StateConnected {
		t.Fatalf("state = %s", client.ConnectionState())
	}
	if client.ProtocolVersion() != protocol.LatestProtocolVersion {
		t.Fatalf("protocolVersion = %s", client.ProtocolVersion())
	}
	if client.ServerInfo() == nil || client.ServerInfo().Name != "test-server" || client.ServerInfo().Version != "1.0.0" {
		t.Fatalf("serverInfo = %+v", client.ServerInfo())
	}
	if client.ServerCapabilities() == nil || client.ServerCapabilities().Tools == nil || !client.ServerCapabilities().Tools.ListChanged {
		t.Fatalf("capabilities = %+v", client.ServerCapabilities())
	}
	if client.Instructions() == nil || *client.Instructions() != "Use test tools." {
		t.Fatalf("instructions = %+v", client.Instructions())
	}
	messages := server.waitForMessages(t, 2)
	if len(messages) != 2 {
		t.Fatalf("messages = %s", jsonNormalize(t, messages))
	}
	_ = server
	wantRequest := map[string]any{
		"jsonrpc": "2.0", "id": float64(1), "method": "initialize",
		"params": map[string]any{
			"protocolVersion": protocol.LatestProtocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test-client", "version": "2.0.0"},
		},
	}
	if jsonNormalize(t, messages[0]) != jsonNormalize(t, wantRequest) {
		t.Fatalf("initialize = %s, want %s", jsonNormalize(t, messages[0]), jsonNormalize(t, wantRequest))
	}
	wantNotification := map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}
	if jsonNormalize(t, messages[1]) != jsonNormalize(t, wantNotification) {
		t.Fatalf("initialized = %s", jsonNormalize(t, messages[1]))
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClientPaginatesTools(t *testing.T) {
	client, server := connectTestClient(t)
	server.setHandler("tools/list", func(request *protocol.JsonRpcRequest) (any, error) {
		var params struct {
			Cursor string `json:"cursor"`
		}
		_ = json.Unmarshal(request.Params, &params)
		if params.Cursor == "" {
			return map[string]any{
				"tools":      []any{map[string]any{"name": "search", "description": "Search", "inputSchema": map[string]any{"type": "object"}}},
				"nextCursor": "page-2",
			}, nil
		}
		return map[string]any{
			"tools": []any{map[string]any{
				"name": "read", "inputSchema": map[string]any{"type": "object"}, "outputSchema": map[string]any{"type": "object"},
				"annotations": map[string]any{"readOnlyHint": true},
			}},
			// Some servers end pagination with an empty cursor instead of
			// omitting it.
			"nextCursor": "",
		}, nil
	})
	tools, err := client.ListTools(context.Background(), RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 || tools[0].Name != "search" || tools[0].Description == nil || *tools[0].Description != "Search" ||
		tools[1].Name != "read" || tools[1].Annotations == nil || tools[1].Annotations.ReadOnlyHint == nil || !*tools[1].Annotations.ReadOnlyHint {
		t.Fatalf("tools = %s", jsonNormalize(t, tools))
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClientListsAndReadsResources(t *testing.T) {
	client, server := connectTestClient(t)
	server.setHandler("resources/list", func(request *protocol.JsonRpcRequest) (any, error) {
		var params struct {
			Cursor string `json:"cursor"`
		}
		_ = json.Unmarshal(request.Params, &params)
		if params.Cursor == "" {
			return map[string]any{
				"resources":  []any{map[string]any{"uri": "file:///a", "name": "a", "mimeType": "text/plain"}},
				"nextCursor": "2",
			}, nil
		}
		return map[string]any{"resources": []any{map[string]any{"uri": "file:///b"}}}, nil
	})
	server.setHandler("resources/templates/list", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{"resourceTemplates": []any{map[string]any{"uriTemplate": "repo://{owner}/{repo}", "name": "repo"}}}, nil
	})
	server.setHandler("resources/read", func(request *protocol.JsonRpcRequest) (any, error) {
		var params struct {
			URI string `json:"uri"`
		}
		_ = json.Unmarshal(request.Params, &params)
		return map[string]any{"contents": []any{map[string]any{"uri": params.URI, "text": "hello"}}}, nil
	})
	resources, err := client.ListResources(context.Background(), RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// A missing name falls back to the URI.
	if len(resources) != 2 || resources[0].URI != "file:///a" || resources[0].Name != "a" || resources[1].Name != "file:///b" {
		t.Fatalf("resources = %s", jsonNormalize(t, resources))
	}
	templates, err := client.ListResourceTemplates(context.Background(), RequestOptions{})
	if err != nil || len(templates) != 1 || templates[0].URITemplate != "repo://{owner}/{repo}" {
		t.Fatalf("templates = %s, err %v", jsonNormalize(t, templates), err)
	}
	page, err := client.ListResourcesPage(context.Background(), nil, RequestOptions{})
	if err != nil || page.NextCursor == nil || *page.NextCursor != "2" || len(page.Resources) != 1 {
		t.Fatalf("page = %+v err %v", page, err)
	}
	page, err = client.ListResourcesPage(context.Background(), new("2"), RequestOptions{})
	if err != nil || page.NextCursor != nil || len(page.Resources) != 1 || page.Resources[0].Name != "file:///b" {
		t.Fatalf("page 2 = %+v err %v", page, err)
	}
	read, err := client.ReadResource(context.Background(), "file:///a", RequestOptions{})
	if err != nil || len(read.Contents) != 1 || read.Contents[0].Text != "hello" {
		t.Fatalf("read = %+v err %v", read, err)
	}

	server.setHandler("resources/read", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{"contents": []any{map[string]any{"uri": "file:///a"}}}, nil
	})
	if _, err := client.ReadResource(context.Background(), "file:///a", RequestOptions{}); err == nil ||
		err.Error() != "Invalid contents in MCP resources/read result" {
		t.Fatalf("read err = %v", err)
	}
	server.setHandler("resources/list", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{"resources": []any{map[string]any{"name": "no uri"}}}, nil
	})
	if _, err := client.ListResources(context.Background(), RequestOptions{}); err == nil ||
		err.Error() != "Invalid entry in MCP resources/list result" {
		t.Fatalf("list err = %v", err)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClientReturnsStructuredToolContentAndSurfacesErrors(t *testing.T) {
	client, server := connectTestClient(t)
	server.setHandler("tools/call", func(request *protocol.JsonRpcRequest) (any, error) {
		var params struct {
			Name      string `json:"name"`
			Arguments struct {
				Count any `json:"count"`
			} `json:"arguments"`
		}
		_ = json.Unmarshal(request.Params, &params)
		if params.Name == "fail" {
			return nil, &protocol.McpError{Code: 1234, Msg: "tool failed", Data: map[string]any{"retryable": false}}
		}
		return map[string]any{
			"content":           []any{map[string]any{"type": "text", "text": "ok"}},
			"structuredContent": map[string]any{"count": params.Arguments.Count},
		}, nil
	})
	result, err := client.CallTool(context.Background(), "count", map[string]any{"count": 3}, RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "text" || result.Content[0].Text != "ok" {
		t.Fatalf("result = %s", jsonNormalize(t, result))
	}
	var structured map[string]any
	if err := json.Unmarshal(result.StructuredContent, &structured); err != nil || structured["count"] != float64(3) {
		t.Fatalf("structured = %s", string(result.StructuredContent))
	}
	if _, err := client.CallTool(context.Background(), "fail", nil, RequestOptions{}); err == nil {
		t.Fatal("fail did not error")
	} else {
		mcpErr, ok := err.(*protocol.McpError)
		if !ok || mcpErr.Code != 1234 || mcpErr.Msg != "tool failed" {
			t.Fatalf("fail err = %v (%T)", err, err)
		}
		var data map[string]any
		if jsonErr := json.Unmarshal(mcpErr.Data.(json.RawMessage), &data); jsonErr != nil || data["retryable"] != false {
			t.Fatalf("data = %s", string(mcpErr.Data.(json.RawMessage)))
		}
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClientAcceptsAnyJSONStructuredToolContent(t *testing.T) {
	client, server := connectTestClient(t)
	values := map[string]json.RawMessage{
		"array":   json.RawMessage(`[1,"two"]`),
		"null":    json.RawMessage(`null`),
		"string":  json.RawMessage(`"value"`),
		"number":  json.RawMessage(`42`),
		"boolean": json.RawMessage(`true`),
	}
	server.setHandler("tools/call", func(request *protocol.JsonRpcRequest) (any, error) {
		var params struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(request.Params, &params)
		return map[string]any{"content": []any{}, "structuredContent": values[params.Name]}, nil
	})
	for name, want := range values {
		result, err := client.CallTool(context.Background(), name, nil, RequestOptions{})
		if err != nil {
			t.Errorf("CallTool(%q): %v", name, err)
			continue
		}
		if string(result.StructuredContent) != string(want) {
			t.Errorf("structuredContent for %q = %s, want %s", name, result.StructuredContent, want)
		}
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClientReportsTransportErrorsWithoutFailingPendingRequests(t *testing.T) {
	server, clientTransport := newTestServer()
	server.setHandler("initialize", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{
			"protocolVersion": protocol.LatestProtocolVersion,
			"capabilities":    map[string]any{},
			"serverInfo":      map[string]any{"name": "test-server", "version": "1.0.0"},
		}, nil
	})
	client := NewClient(ClientOptions{Name: "test-client", Version: "1.0.0"})
	if _, err := client.Connect(context.Background(), clientTransport); err != nil {
		t.Fatal(err)
	}
	var errorMessages []string
	client.OnError(func(err error) { errorMessages = append(errorMessages, err.Error()) })
	respond := make(chan struct{})
	server.setHandler("tools/call", func(*protocol.JsonRpcRequest) (any, error) {
		<-respond
		return map[string]any{"content": []any{}}, nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := client.CallTool(context.Background(), "wait", nil, RequestOptions{})
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	clientTransport.EmitError(errors.New("stray log line"))
	close(respond)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("call did not complete")
	}
	if len(errorMessages) != 1 || errorMessages[0] != "stray log line" {
		t.Fatalf("errors = %v", errorMessages)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClientAcceptsOlderProtocolVersions(t *testing.T) {
	server, clientTransport := newTestServer()
	server.setHandler("initialize", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"serverInfo":      map[string]any{"name": "old-server", "version": "0.1.0"},
		}, nil
	})
	client := NewClient(ClientOptions{Name: "test-client", Version: "1.0.0"})
	if _, err := client.Connect(context.Background(), clientTransport); err != nil {
		t.Fatal(err)
	}
	if client.ProtocolVersion() != "2024-11-05" {
		t.Fatalf("protocolVersion = %s", client.ProtocolVersion())
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	unsupportedServer, unsupportedTransport := newTestServer()
	unsupportedServer.setHandler("initialize", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{
			"protocolVersion": "1999-01-01",
			"capabilities":    map[string]any{},
			"serverInfo":      map[string]any{"name": "ancient-server", "version": "0.1.0"},
		}, nil
	})
	rejected := NewClient(ClientOptions{Name: "test-client", Version: "1.0.0"})
	if _, err := rejected.Connect(context.Background(), unsupportedTransport); err == nil ||
		!strings.Contains(err.Error(), "unsupported protocol version") {
		t.Fatalf("connect err = %v", err)
	}
	if rejected.ConnectionState() != StateClosed {
		t.Fatalf("state = %s", rejected.ConnectionState())
	}
}

// Reference: pi v1.4.0 packages/mcp/test/client.test.ts expects missing
// content to default to a non-nil empty array.
func TestClientDefaultsMissingToolResultContent(t *testing.T) {
	client, server := connectTestClient(t)
	server.setHandler("tools/call", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{"structuredContent": map[string]any{"ok": true}}, nil
	})
	result, err := client.CallTool(context.Background(), "structured", nil, RequestOptions{})
	if err != nil || result.Content == nil || len(result.Content) != 0 {
		t.Fatalf("result = %s err %v", jsonNormalize(t, result), err)
	}
	var structured map[string]any
	if err := json.Unmarshal(result.StructuredContent, &structured); err != nil || structured["ok"] != true {
		t.Fatalf("structured = %s", string(result.StructuredContent))
	}
	server.setHandler("tools/call", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{"content": "not a list"}, nil
	})
	if _, err := client.CallTool(context.Background(), "broken", nil, RequestOptions{}); err == nil ||
		err.Error() != "Invalid MCP tools/call result" {
		t.Fatalf("broken err = %v", err)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// waitForMessages polls until the server has recorded at least n messages
// (in-memory sends deliver on their own goroutine).
func (s *testServer) waitForMessages(t *testing.T, n int) []Message {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		messages := s.recordedMessages()
		if len(messages) >= n {
			return messages
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d messages recorded: %s", len(messages), n, jsonNormalize(t, messages))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// client.ts validates initialize members with typeof checks: empty strings
// are still strings, while `instructions: null` is not.
func TestClientInitializeValidationUsesJavaScriptTypeChecks(t *testing.T) {
	server, clientTransport := newTestServer()
	server.setHandler("initialize", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{
			"protocolVersion": protocol.LatestProtocolVersion,
			"capabilities":    map[string]any{},
			"serverInfo":      map[string]any{"name": "", "version": ""},
		}, nil
	})
	client := NewClient(ClientOptions{Name: "test-client", Version: "1.0.0"})
	if _, err := client.Connect(context.Background(), clientTransport); err != nil {
		t.Fatalf("empty serverInfo strings must still be strings: %v", err)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	server, clientTransport = newTestServer()
	server.setHandler("initialize", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{
			"protocolVersion": protocol.LatestProtocolVersion,
			"capabilities":    map[string]any{},
			"serverInfo":      map[string]any{"name": "s", "version": "1"},
			"instructions":    nil,
		}, nil
	})
	client = NewClient(ClientOptions{Name: "test-client", Version: "1.0.0"})
	if _, err := client.Connect(context.Background(), clientTransport); err == nil ||
		err.Error() != "Invalid MCP initialize result" {
		t.Fatalf("null instructions err = %v; want Invalid MCP initialize result", err)
	}
}

// Reference: pi v1.4.0 packages/mcp/src/client.ts validateListPage returns the
// original parsed tool objects, preserving inputSchema property enumeration
// order rather than rebuilding the object through an unordered map.
func TestClientListToolsPreservesInputSchemaPropertyOrder(t *testing.T) {
	client, server := connectTestClient(t)
	defer func() {
		if err := client.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()
	server.setHandler("tools/list", func(*protocol.JsonRpcRequest) (any, error) {
		return json.RawMessage(`{"tools":[{"name":"ordered","inputSchema":{"type":"object","properties":{"z":{"type":"string"},"a":{"type":"integer"}}}}]}`), nil
	})

	tools, err := client.ListTools(context.Background(), RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"type":"object","properties":{"z":{"type":"string"},"a":{"type":"integer"}}}`
	if len(tools) != 1 || string(tools[0].InputSchema) != want {
		if len(tools) == 1 {
			t.Fatalf("input schema = %s, want %s", tools[0].InputSchema, want)
		}
		t.Fatalf("tools = %+v, want one ordered tool", tools)
	}
}

// Reference: pi v1.4.0 (4a080252), packages/mcp/src/client.ts
// requestInternal and stdio.ts/streamable-http.ts propagate JSON serialization
// failures even when requestInternal adds a progress token.
func TestClientRejectsCyclicArgumentsWithProgress(t *testing.T) {
	for _, progress := range []bool{false, true} {
		name := "plain"
		if progress {
			name = "progress"
		}
		t.Run(name, func(t *testing.T) {
			client, server := connectTestClient(t)
			defer client.Close(context.Background())
			server.setHandler("tools/call", func(*protocol.JsonRpcRequest) (any, error) { return map[string]any{"content": []any{}}, nil })
			args := map[string]any{}
			args["self"] = args
			options := RequestOptions{}
			if progress {
				options.OnProgress = func(*protocol.ProgressNotification) {}
			}
			_, err := client.CallTool(context.Background(), "echo", args, options)
			if err == nil || !strings.Contains(err.Error(), "encountered a cycle") {
				t.Fatalf("cyclic arguments error = %v, want serialization failure", err)
			}
			for _, message := range server.recordedMessages() {
				if request, ok := message.(*protocol.JsonRpcRequest); ok && request.Method == "tools/call" {
					t.Fatalf("unserializable tool call reached server: %+v", request)
				}
			}
		})
	}
}

// Reference: pi v1.4.0 packages/mcp/src/client.ts listResourcesPage maps
// zero validated entries to a non-nil empty resources array.
func TestClientEmptyResourcePageReturnsEmptySlice(t *testing.T) {
	client, server := connectTestClient(t)
	defer func() {
		if err := client.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()
	server.setHandler("resources/list", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{"resources": []any{}}, nil
	})

	page, err := client.ListResourcesPage(context.Background(), nil, RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Resources == nil || len(page.Resources) != 0 {
		t.Fatalf("resources = %#v, want non-nil empty slice", page.Resources)
	}
}

// Reference: pi v1.4.0 packages/mcp/src/client.ts listResourceTemplatesPage
// maps zero validated entries to a non-nil empty resourceTemplates array.
func TestClientEmptyResourceTemplatePageReturnsEmptySlice(t *testing.T) {
	client, server := connectTestClient(t)
	defer func() {
		if err := client.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()
	server.setHandler("resources/templates/list", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{"resourceTemplates": []any{}}, nil
	})

	page, err := client.ListResourceTemplatesPage(context.Background(), nil, RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if page.ResourceTemplates == nil || len(page.ResourceTemplates) != 0 {
		t.Fatalf("resourceTemplates = %#v, want non-nil empty slice", page.ResourceTemplates)
	}
}
