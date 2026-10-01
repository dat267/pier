package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

// Port of packages/mcp/test/client.test.ts: the in-memory pair plays the
// server, capturing every message and answering from per-method handlers.

type testServer struct {
	t          *testing.T
	transport  *InMemoryTransport
	mu         sync.Mutex
	messages   []Message
	handlers   map[string]func(*protocol.JsonRpcRequest) (any, error)
	connecting bool
}

func newTestServer() (*testServer, *InMemoryTransport) {
	clientTransport, serverTransport := CreateInMemoryTransportPair()
	server := &testServer{t: &testing.T{}, transport: serverTransport, handlers: map[string]func(*protocol.JsonRpcRequest) (any, error){}}
	serverTransport.OnMessage(func(message Message) {
		server.mu.Lock()
		server.messages = append(server.messages, message)
		server.mu.Unlock()
		request, ok := message.(*protocol.JsonRpcRequest)
		if !ok {
			return
		}
		server.mu.Lock()
		handler := server.handlers[request.Method]
		server.mu.Unlock()
		go func() {
			var result any
			var failure *protocol.McpError
			if handler == nil {
				failure = &protocol.McpError{Code: protocol.JSONRPCErrorCode.MethodNotFound, Msg: fmt.Sprintf("Method not found: %s", request.Method)}
			} else {
				value, err := handler(request)
				if err != nil {
					if mcpErr, ok := err.(*protocol.McpError); ok {
						failure = mcpErr
					} else {
						failure = &protocol.McpError{Code: protocol.JSONRPCErrorCode.InternalError, Msg: err.Error()}
					}
				} else {
					result = value
				}
			}
			if failure != nil {
				data, _ := json.Marshal(failure.Data)
				_ = serverTransport.Send(context.Background(), protocol.JsonRpcResponse{
					JSONRPC: protocol.JSONRPCVersion, ID: request.ID,
					Error: &protocol.JsonRpcErrorObject{Code: failure.Code, Message: failure.Msg, Data: data},
				})
				return
			}
			enc, err := json.Marshal(result)
			if err != nil {
				panic(err)
			}
			_ = serverTransport.Send(context.Background(), protocol.JsonRpcResponse{
				JSONRPC: protocol.JSONRPCVersion, ID: request.ID, Result: enc,
			})
		}()
	})
	_ = serverTransport.Start(context.Background())
	return server, clientTransport
}

func (s *testServer) setHandler(method string, handler func(*protocol.JsonRpcRequest) (any, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = handler
}

func (s *testServer) recordedMessages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message{}, s.messages...)
}

func connectTestClient(t *testing.T) (*Client, *testServer) {
	t.Helper()
	server, clientTransport := newTestServer()
	server.setHandler("initialize", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{
			"protocolVersion": protocol.LatestProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": true}},
			"serverInfo":      map[string]any{"name": "test-server", "version": "1.0.0"},
			"instructions":    "Use test tools.",
		}, nil
	})
	client := NewClient(ClientOptions{Name: "test-client", Version: "2.0.0"})
	if _, err := client.Connect(context.Background(), clientTransport); err != nil {
		t.Fatal(err)
	}
	return client, server
}

// jsonNormalize decodes both sides so Go map ordering does not matter.
func jsonNormalize(t *testing.T, value any) string {
	t.Helper()
	enc, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var normalized any
	if err := json.Unmarshal(enc, &normalized); err != nil {
		t.Fatal(err)
	}
	enc, err = json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	return string(enc)
}

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
	page, err = client.ListResourcesPage(context.Background(), strPtr("2"), RequestOptions{})
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

func TestClientRenewsTimeoutOnProgress(t *testing.T) {
	client, server := connectTestClient(t)
	server.setHandler("tools/call", func(request *protocol.JsonRpcRequest) (any, error) {
		var params struct {
			Meta struct {
				ProgressToken int64 `json:"progressToken"`
			} `json:"_meta"`
		}
		_ = json.Unmarshal(request.Params, &params)
		token := params.Meta.ProgressToken
		time.AfterFunc(40*time.Millisecond, func() {
			enc, _ := json.Marshal(map[string]any{"progressToken": token, "progress": 1, "total": 2})
			_ = server.transport.Send(context.Background(), protocol.JsonRpcNotification{
				JSONRPC: protocol.JSONRPCVersion, Method: "notifications/progress", Params: enc,
			})
		})
		time.Sleep(80 * time.Millisecond)
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": "done"}}}, nil
	})
	var mu sync.Mutex
	var progress []*protocol.ProgressNotification
	go func() {
		// The request runs with a 50ms timeout that progress renews; a
		// regression to the non-renewing timer would time out at 50ms.
		result, err := client.CallTool(context.Background(), "slow", map[string]any{}, RequestOptions{
			TimeoutMs: 50,
			OnProgress: func(notification *protocol.ProgressNotification) {
				mu.Lock()
				progress = append(progress, notification)
				mu.Unlock()
			},
		})
		if err != nil {
			t.Errorf("slow call: %v", err)
			return
		}
		if len(result.Content) != 1 || result.Content[0].Text != "done" {
			t.Errorf("result = %s", jsonNormalize(t, result))
		}
	}()
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(progress) != 1 || progress[0].Progress != 1 || progress[0].Total == nil || *progress[0].Total != 2 {
		t.Fatalf("progress = %s", jsonNormalize(t, progress))
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClientCancelsAbortedAndTimedOutRequests(t *testing.T) {
	client, server := connectTestClient(t)
	server.setHandler("tools/call", func(*protocol.JsonRpcRequest) (any, error) {
		select {}
	})
	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.CallTool(ctx, "wait", map[string]any{}, RequestOptions{})
		done <- err
	}()
	time.Sleep(20 * time.Millisecond) // let the request reach the server first
	cancel(errors.New("stop"))
	select {
	case err := <-done:
		var abortErr *protocol.McpAbortError
		if !errors.As(err, &abortErr) {
			t.Fatalf("abort err = %v (%T)", err, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("aborted call did not return")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		found := false
		for _, message := range server.recordedMessages() {
			if notification, ok := message.(*protocol.JsonRpcNotification); ok && notification.Method == "notifications/cancelled" {
				var params struct {
					RequestID float64 `json:"requestId"`
					Reason    string  `json:"reason"`
				}
				_ = json.Unmarshal(notification.Params, &params)
				if params.RequestID == 2 && params.Reason == "stop" {
					found = true
				}
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no notifications/cancelled with requestId 2; messages = %s", jsonNormalize(t, server.recordedMessages()))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := client.CallTool(context.Background(), "wait", map[string]any{}, RequestOptions{TimeoutMs: 5}); err == nil {
		t.Fatal("timed-out call did not error")
	} else {
		var timeoutErr *protocol.McpTimeoutError
		if !errors.As(err, &timeoutErr) {
			t.Fatalf("timeout err = %v (%T)", err, err)
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

func TestClientDefaultsMissingToolResultContent(t *testing.T) {
	client, server := connectTestClient(t)
	server.setHandler("tools/call", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{"structuredContent": map[string]any{"ok": true}}, nil
	})
	result, err := client.CallTool(context.Background(), "structured", nil, RequestOptions{})
	if err != nil || result.Content != nil && len(result.Content) != 0 {
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

func TestClientTimedOutInitializeSendsNoCancellation(t *testing.T) {
	server, clientTransport := newTestServer()
	server.setHandler("initialize", func(*protocol.JsonRpcRequest) (any, error) {
		select {}
	})
	client := NewClient(ClientOptions{Name: "test-client", Version: "1.0.0", RequestTimeoutMs: 5})
	if _, err := client.Connect(context.Background(), clientTransport); err == nil {
		t.Fatal("connect did not time out")
	} else {
		var timeoutErr *protocol.McpTimeoutError
		if !errors.As(err, &timeoutErr) {
			t.Fatalf("connect err = %v (%T)", err, err)
		}
	}
	for _, message := range server.recordedMessages() {
		if notification, ok := message.(*protocol.JsonRpcNotification); ok && notification.Method == "notifications/cancelled" {
			t.Fatalf("cancellation sent: %s", jsonNormalize(t, notification))
		}
	}
}

func TestClientNotifiesCloseListenersOnceOnTransportDrop(t *testing.T) {
	client, server := connectTestClient(t)
	closeCount := 0
	client.OnClose(func() { closeCount++ })
	server.setHandler("tools/call", func(*protocol.JsonRpcRequest) (any, error) {
		select {}
	})
	done := make(chan error, 1)
	go func() {
		_, err := client.CallTool(context.Background(), "wait", nil, RequestOptions{})
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	if err := server.transport.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || err.Error() != "MCP connection closed" {
			t.Fatalf("pending err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending call did not reject")
	}
	if client.ConnectionState() != StateClosed {
		t.Fatalf("state = %s", client.ConnectionState())
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if closeCount != 1 {
		t.Fatalf("close listeners = %d", closeCount)
	}
}

func TestClientAnswersRootsListAndDispatchesNotifications(t *testing.T) {
	server, clientTransport := newTestServer()
	server.setHandler("initialize", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{
			"protocolVersion": protocol.LatestProtocolVersion,
			"capabilities":    map[string]any{},
			"serverInfo":      map[string]any{"name": "test-server", "version": "1.0.0"},
		}, nil
	})
	client := NewClient(ClientOptions{
		Name:    "test-client",
		Version: "1.0.0",
		Roots:   []protocol.Root{{URI: "file:///workspace", Name: strPtr("workspace")}},
	})
	if _, err := client.Connect(context.Background(), clientTransport); err != nil {
		t.Fatal(err)
	}
	changed := make(chan json.RawMessage, 1)
	client.OnNotification("notifications/tools/list_changed", func(params json.RawMessage) {
		changed <- params
	})
	rootsRequest := protocol.JsonRpcRequest{JSONRPC: protocol.JSONRPCVersion, ID: protocol.StringId("roots"), Method: "roots/list"}
	if err := server.transport.Send(context.Background(), rootsRequest); err != nil {
		t.Fatal(err)
	}
	if err := server.transport.Send(context.Background(), protocol.JsonRpcNotification{
		JSONRPC: protocol.JSONRPCVersion, Method: "notifications/tools/list_changed",
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		answered := false
		for _, message := range server.recordedMessages() {
			if response, ok := message.(*protocol.JsonRpcResponse); ok && response.ID.Str == "roots" && response.Error == nil {
				want := jsonNormalize(t, map[string]any{"roots": []any{map[string]any{"uri": "file:///workspace", "name": "workspace"}}})
				if jsonNormalize(t, response.Result) != want {
					t.Fatalf("roots result = %s, want %s", string(response.Result), want)
				}
				answered = true
			}
		}
		if answered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("roots/list unanswered; messages = %s", jsonNormalize(t, server.recordedMessages()))
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case <-changed:
	case <-time.After(2 * time.Second):
		t.Fatal("tools/list_changed not dispatched")
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func strPtr(s string) *string { return &s }

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
