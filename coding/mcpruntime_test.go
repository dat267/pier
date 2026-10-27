package coding

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/mcp"
	"github.com/dat267/pier/mcp/protocol"
)

// Port of the "MCP connections" cases of upstream
// packages/coding-agent/test/mcp-extension.test.ts, plus the tool definition
// from createMcpToolDefinition.

// mcpFakeServer answers initialize, tools/list and tools/call (upstream's
// in-memory server).
type mcpFakeServer struct {
	mu           sync.Mutex
	methods      []string
	noTools      bool
	hasResources bool
	calls        int
	onCall       func()
	instructions string
	transport    *mcp.InMemoryTransport
}

func newMCPFakeServer(t *testing.T, noTools bool) (*mcpFakeServer, *mcp.InMemoryTransport) {
	t.Helper()
	client, server := mcp.CreateInMemoryTransportPair()
	fake := &mcpFakeServer{noTools: noTools, transport: server}
	server.OnMessage(func(message mcp.Message) {
		request, ok := message.(*protocol.JsonRpcRequest)
		if !ok {
			return
		}
		fake.mu.Lock()
		fake.methods = append(fake.methods, request.Method)
		fake.mu.Unlock()
		go func() {
			var result any
			var failure *protocol.McpError
			switch {
			case request.Method == "initialize":
				capabilities := map[string]any{"tools": map[string]any{}}
				if fake.noTools {
					capabilities = map[string]any{"prompts": map[string]any{}}
				}
				if fake.hasResources {
					capabilities["resources"] = map[string]any{}
				}
				result = map[string]any{
					"protocolVersion": protocol.LatestProtocolVersion,
					"capabilities":    capabilities,
					"serverInfo":      map[string]any{"name": "fake", "version": "1.0.0"},
					"instructions":    fake.instructions,
				}
			case request.Method == "tools/list":
				if fake.noTools {
					failure = &protocol.McpError{Code: protocol.JSONRPCErrorCode.MethodNotFound, Msg: "Method not found"}
				} else {
					result = map[string]any{"tools": []any{
						map[string]any{"name": "echo", "description": "Echo", "inputSchema": map[string]any{"type": "object"}},
					}}
				}
			case request.Method == "tools/call":
				fake.mu.Lock()
				fake.calls++
				onCall := fake.onCall
				fake.mu.Unlock()
				if onCall != nil {
					onCall()
				}
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

func (s *mcpFakeServer) recordedMethods() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.methods...)
}

// failingTransport injects one error into the next request of one method
// (upstream's send-override tests). An empty method fails the next request.
type failingTransport struct {
	mcp.Transport
	mu     sync.Mutex
	failed bool
	method string
	err    error
}

type closeTrackingTransport struct {
	mcp.Transport
	closed chan struct{}
	once   sync.Once
}

func (t *closeTrackingTransport) Close(ctx context.Context) error {
	t.once.Do(func() { close(t.closed) })
	return t.Transport.Close(ctx)
}

func (t *failingTransport) Send(ctx context.Context, message mcp.Message) error {
	request, ok := message.(*protocol.JsonRpcRequest)
	if !ok {
		return t.Transport.Send(ctx, message)
	}
	if t.method != "" && request.Method != t.method {
		return t.Transport.Send(ctx, message)
	}
	t.mu.Lock()
	already := t.failed
	t.failed = true
	t.mu.Unlock()
	if !already {
		return t.err
	}
	return t.Transport.Send(ctx, message)
}

// mcpTestConnection builds a connection whose transports come from the factory
// list, in order (upstream's connect()).
func mcpTestConnection(t *testing.T, config McpServerConfig, transports []func() mcp.Transport) (*McpServerConnection, func() int) {
	t.Helper()
	var mu sync.Mutex
	opened := 0
	connection := NewMcpServerConnection(McpServerConnectionOptions{
		Entry: McpServerEntry{Name: "fake", Config: &config, Source: "test", Scope: McpScopeExtension},
		Cwd:   t.TempDir(),
		CreateTransport: func(McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error) {
			mu.Lock()
			defer mu.Unlock()
			if opened >= len(transports) {
				t.Errorf("transport factory called %d times, only %d prepared", opened+1, len(transports))
				return nil, errors.New("no transport")
			}
			transport := transports[opened]()
			opened++
			return transport, nil
		},
		Credentials: NewMemoryMcpOAuthCredentialStore(),
	})
	t.Cleanup(func() { _ = connection.Close(context.Background()) })
	return connection, func() int {
		mu.Lock()
		defer mu.Unlock()
		return opened
	}
}

func TestMcpConnectionCloseWaitsForInFlightConnect(t *testing.T) {
	clientTransport, serverTransport := mcp.CreateInMemoryTransportPair()
	initializeReceived := make(chan struct{})
	var initializeOnce sync.Once
	serverTransport.OnMessage(func(message mcp.Message) {
		request, ok := message.(*protocol.JsonRpcRequest)
		if ok && request.Method == "initialize" {
			initializeOnce.Do(func() { close(initializeReceived) })
		}
	})
	if err := serverTransport.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	transport := &closeTrackingTransport{Transport: clientTransport, closed: make(chan struct{})}
	connection := NewMcpServerConnection(McpServerConnectionOptions{
		Entry: McpServerEntry{Name: "slow", Config: &McpServerConfig{Command: "unused"}},
		CreateTransport: func(McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error) {
			return transport, nil
		},
		Credentials: NewMemoryMcpOAuthCredentialStore(),
	})
	getDone := make(chan error, 1)
	go func() { _, err := connection.GetClient(context.Background()); getDone <- err }()
	select {
	case <-initializeReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("connect never reached initialize")
	}
	t.Cleanup(func() { _ = transport.Close(context.Background()) })

	if err := connection.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-transport.closed:
	default:
		t.Fatal("Close returned before closing in-flight transport")
	}
	select {
	case <-getDone:
	case <-time.After(time.Second):
		t.Fatal("Close returned before in-flight connect settled")
	}
}

// TestMcpConnectionRetriesExpiredSession covers upstream's session-expiry
// retry: the call was not run, so it starts a new session and retries once.
func TestMcpConnectionRetriesExpiredSession(t *testing.T) {
	config := McpServerConfig{Command: "unused"}
	connection, opened := mcpTestConnection(t, config, []func() mcp.Transport{
		func() mcp.Transport {
			_, client := newMCPFakeServer(t, false)
			return &failingTransport{Transport: client, method: "tools/call", err: &mcp.McpSessionExpiredError{Status: 404, Msg: "gone"}}
		},
		func() mcp.Transport {
			_, client := newMCPFakeServer(t, false)
			return client
		},
	})
	resultA, errA := connection.CallTool(context.Background(), "echo", map[string]any{}, mcp.RequestOptions{})
	resultB, errB := connection.CallTool(context.Background(), "echo", map[string]any{}, mcp.RequestOptions{})
	for _, err := range []error{errA, errB} {
		if err != nil {
			t.Fatalf("call: %v", err)
		}
	}
	for _, result := range []*protocol.CallToolResult{resultA, resultB} {
		if len(result.Content) != 1 || result.Content[0].Text != "ok" {
			t.Fatalf("result = %+v", result)
		}
	}
	if opened() != 2 {
		t.Fatalf("opened = %d", opened())
	}
}

func TestMcpConnectionDoesNotListResourcesAtConnect(t *testing.T) {
	fake, client := newMCPFakeServer(t, false)
	fake.hasResources = true
	connection, _ := mcpTestConnection(t, McpServerConfig{Command: "unused"}, []func() mcp.Transport{
		func() mcp.Transport { return client },
	})
	if _, err := connection.GetClient(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !connection.HasResources {
		t.Fatal("server resource capability was not retained")
	}
	if got := strings.Join(fake.recordedMethods(), ","); got != "initialize,tools/list" {
		t.Fatalf("connect methods = %q, want initialize,tools/list", got)
	}
}

// TestMcpConnectionWithoutToolsCapability covers a server that does not answer
// tools/list.
func TestMcpConnectionWithoutToolsCapability(t *testing.T) {
	fake, client := newMCPFakeServer(t, true)
	config := McpServerConfig{Command: "unused"}
	connection, _ := mcpTestConnection(t, config, []func() mcp.Transport{func() mcp.Transport { return client }})
	if _, err := connection.GetClient(context.Background()); err != nil {
		t.Fatal(err)
	}
	if connection.State != McpServerConnected {
		t.Fatalf("state = %s", connection.State)
	}
	if len(connection.Tools) != 0 {
		t.Fatalf("tools = %+v", connection.Tools)
	}
	if got := strings.Join(fake.recordedMethods(), ","); got != "initialize" {
		t.Fatalf("methods = %s", got)
	}
	if !connection.HasResources == false {
		t.Fatalf("hasResources = %v", connection.HasResources)
	}
}

// TestMcpConnectionMarksDropAndReconnects covers a dropped transport: the next
// call reconnects.
func TestMcpConnectionMarksDropAndReconnects(t *testing.T) {
	servers := []*mcpFakeServer{}
	makeTransport := func(t *testing.T) mcp.Transport {
		fake, client := newMCPFakeServer(t, false)
		servers = append(servers, fake)
		return client
	}
	config := McpServerConfig{Command: "unused"}
	var first mcp.Transport
	connection, opened := mcpTestConnection(t, config, []func() mcp.Transport{
		func() mcp.Transport { first = makeTransport(t); return first },
		func() mcp.Transport { return makeTransport(t) },
	})
	if _, err := connection.GetClient(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The server side drops, which closes the client transport.
	_ = first.Close(context.Background())
	waitFor(t, func() bool { return connection.State == McpServerDisconnected })
	if connection.Error != "Connection closed" {
		t.Fatalf("error = %q", connection.Error)
	}
	result, err := connection.CallTool(context.Background(), "echo", map[string]any{}, mcp.RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "ok" {
		t.Fatalf("result = %+v", result)
	}
	if connection.State != McpServerConnected {
		t.Fatalf("state = %s", connection.State)
	}
	if opened() != 2 {
		t.Fatalf("opened = %d", opened())
	}
}

// TestMcpConnectionRetriesTransientHTTPConnect covers the HTTP connect retry
// and the non-transient failure path.
func TestMcpConnectionRetriesTransientHTTPConnect(t *testing.T) {
	config := McpServerConfig{URL: "http://unused.invalid", Headers: map[string]string{"Authorization": "x"}}
	connection, opened := mcpTestConnection(t, config, []func() mcp.Transport{
		func() mcp.Transport {
			_, client := newMCPFakeServer(t, false)
			return &failingTransport{
				Transport: client, method: "initialize",
				err: &mcp.McpHttpError{Status: 503, Msg: "MCP HTTP request failed with status 503"},
			}
		},
		func() mcp.Transport {
			_, client := newMCPFakeServer(t, false)
			return client
		},
	})
	if _, err := connection.GetClient(context.Background()); err != nil {
		t.Fatal(err)
	}
	if connection.State != McpServerConnected {
		t.Fatalf("state = %s", connection.State)
	}
	if opened() != 2 {
		t.Fatalf("opened = %d", opened())
	}

	failing, failingOpened := mcpTestConnection(t, config, []func() mcp.Transport{
		func() mcp.Transport {
			_, client := newMCPFakeServer(t, false)
			return &failingTransport{
				Transport: client, method: "initialize",
				err: &mcp.McpHttpError{Status: 400, Msg: "MCP HTTP request failed with status 400: bad"},
			}
		},
	})
	_, err := failing.GetClient(context.Background())
	if err == nil || !strings.Contains(err.Error(), "status 400: bad") {
		t.Fatalf("err = %v", err)
	}
	if failing.State != McpServerFailed {
		t.Fatalf("state = %s", failing.State)
	}
	if failingOpened() != 1 {
		t.Fatalf("opened = %d", failingOpened())
	}
}

// TestMcpConnectionAsksOAuthServersToSignIn covers the 401 -> needs-auth path:
// OAuth servers that keep rejecting requests ask for a new sign-in.
func TestMcpConnectionAsksOAuthServersToSignIn(t *testing.T) {
	config := McpServerConfig{URL: "http://unused.invalid"}
	challenge := "Bearer resource_metadata=\"https://example.com/meta\""
	connection, _ := mcpTestConnection(t, config, []func() mcp.Transport{
		func() mcp.Transport {
			_, client := newMCPFakeServer(t, false)
			return &failingTransport{
				Transport: client, method: "initialize",
				err: &mcp.McpAuthRequiredError{
					Status: 401, Msg: "unauthorized",
					WWWAuthenticate: challenge,
				},
			}
		},
	})
	_, err := connection.GetClient(context.Background())
	if err == nil || !strings.Contains(err.Error(), "requires sign-in") {
		t.Fatalf("err = %v", err)
	}
	if connection.State != McpServerNeedsAuth {
		t.Fatalf("state = %s", connection.State)
	}
	// The port has no /mcp sign-in command (D185), so the message must not send
	// the user to one: it names the mcp.json fields that authenticate a server.
	if message := err.Error(); strings.Contains(message, "/mcp") || !strings.Contains(message, "mcp.json") {
		t.Fatalf("message = %q", message)
	}
}

// TestCreateMcpToolDefinition covers the tool definition's name, label,
// description fallback, schema normalization, execution and error result.
func TestCreateMcpToolDefinition(t *testing.T) {
	description := "Echo"
	tool := protocol.Tool{Name: "echo", Description: &description, InputSchema: json.RawMessage(`{"properties":{"text":{"type":"string"}}}`)}
	var called map[string]any
	toolDefinition := CreateMcpToolDefinition(McpToolDefinitionOptions{
		Server: "docs", Tool: tool, Name: "mcp__docs__echo", Exposure: McpExposureDirect, TimeoutMs: 1000,
		GetClient: func(context.Context) (McpToolCaller, error) {
			return mcpToolCallerFunc(func(_ context.Context, name string, args map[string]any, _ mcp.RequestOptions) (*protocol.CallToolResult, error) {
				called = args
				return &protocol.CallToolResult{Content: []protocol.ContentBlock{{Type: protocol.ContentTypeText, Text: "ok"}}}, nil
			}), nil
		},
	})
	if toolDefinition.Name != "mcp__docs__echo" || toolDefinition.Label != "docs/echo" {
		t.Fatalf("definition = %+v", toolDefinition)
	}
	if toolDefinition.Description != "Echo" {
		t.Fatalf("description = %q", toolDefinition.Description)
	}
	var parameters map[string]any
	if err := json.Unmarshal(toolDefinition.Parameters, &parameters); err != nil {
		t.Fatal(err)
	}
	if parameters["type"] != "object" {
		t.Fatalf("parameters = %v", parameters)
	}
	result, err := toolDefinition.Execute("call-1", json.RawMessage(`{"text":"hi"}`), context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if called["text"] != "hi" {
		t.Fatalf("args = %v", called)
	}
	if len(result.Content) != 1 || result.Content[0].(ai.TextContent).Text != "ok" {
		t.Fatalf("result = %+v", result.Content)
	}

	// Annotations.title is the fallback when the top-level title is absent.
	annotationTitle := "Annotated tool"
	annotated := CreateMcpToolDefinition(McpToolDefinitionOptions{
		Server: "docs", Tool: protocol.Tool{Name: "annotated", Annotations: &protocol.ToolAnnotations{Title: &annotationTitle}}, Name: "mcp__docs__annotated",
	})
	if annotated.Description != annotationTitle {
		t.Fatalf("annotated description = %q; want %q", annotated.Description, annotationTitle)
	}

	// A description without text falls back to the server/tool label.
	unnamed := CreateMcpToolDefinition(McpToolDefinitionOptions{
		Server: "docs", Tool: protocol.Tool{Name: "mystery"}, Name: "mcp__docs__mystery",
		GetClient: func(context.Context) (McpToolCaller, error) { return nil, errors.New("unused") },
	})
	if unnamed.Description != "MCP tool mystery from server docs" {
		t.Fatalf("description = %q", unnamed.Description)
	}

	// An isError result keeps its converted content and details and flags the
	// result (upstream convertMcpResult returns them with isError: true), instead
	// of replacing both with an error string.
	erroring := CreateMcpToolDefinition(McpToolDefinitionOptions{
		Server: "docs", Tool: tool, Name: "mcp__docs__echo",
		GetClient: func(context.Context) (McpToolCaller, error) {
			return mcpToolCallerFunc(func(context.Context, string, map[string]any, mcp.RequestOptions) (*protocol.CallToolResult, error) {
				return &protocol.CallToolResult{
					Content: []protocol.ContentBlock{{Type: protocol.ContentTypeText, Text: "nope"}},
					IsError: true,
				}, nil
			}), nil
		},
	})
	errorResult, err := erroring.Execute("call-2", json.RawMessage(`{}`), context.Background(), nil)
	if err != nil {
		t.Fatalf("err = %v; want the isError result, not a Go error", err)
	}
	if !errorResult.IsError {
		t.Fatal("result.IsError = false; want true")
	}
	if len(errorResult.Content) != 1 || errorResult.Content[0].(ai.TextContent).Text != "nope" {
		t.Fatalf("content = %+v", errorResult.Content)
	}
	if details := string(errorResult.Details); !strings.Contains(details, `"tool":"echo"`) || !strings.Contains(details, `"server":"docs"`) {
		t.Fatalf("details = %s; want the MCP tool details preserved", details)
	}
}

// mcpToolCallerFunc adapts a function to McpToolCaller.
type mcpToolCallerFunc func(ctx context.Context, name string, args map[string]any, options mcp.RequestOptions) (*protocol.CallToolResult, error)

func (f mcpToolCallerFunc) CallTool(ctx context.Context, name string, args map[string]any, options mcp.RequestOptions) (*protocol.CallToolResult, error) {
	return f(ctx, name, args, options)
}

// waitFor polls a condition (the port has no async event loop in these tests).
func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestMcpConfigValueAndHomeExpansion covers upstream's shell-style config
// values (`${NAME}`, `!cmd`) and `~` expansion.
func TestMcpConfigValueAndHomeExpansion(t *testing.T) {
	t.Setenv("MCP_TEST_TOKEN", "secret")
	if got, err := ResolveMcpConfigValue("Bearer ${MCP_TEST_TOKEN}"); err != nil || got != "Bearer secret" {
		t.Fatalf("value = %q err = %v", got, err)
	}
	if got, err := ResolveMcpConfigValue("${MCP_TEST_MISSING}"); err != nil || got != "" {
		t.Fatalf("missing env = %q err = %v", got, err)
	}
	output, err := ResolveMcpConfigValue("!printf hello")
	if err != nil {
		t.Fatal(err)
	}
	if output != "hello" {
		t.Fatalf("command = %q", output)
	}
	if output, err := ResolveMcpConfigValue(`!printf '\377'`); err != nil || output != "\uFFFD" {
		t.Fatalf("malformed UTF-8 command output = %q, %v", output, err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := expandMcpHome("~/work"); got != home+"/work" {
		t.Fatalf("home = %q", got)
	}
	if got := expandMcpHome("plain"); got != "plain" {
		t.Fatalf("plain = %q", got)
	}
	if got := expandMcpHome("~"); got != home {
		t.Fatalf("tilde = %q", got)
	}
}

// Upstream parseConfigValueTemplate handles "$$"/"$!" escapes and only treats a
// value starting with "!" (no trimming) as a command.
func TestResolveMcpConfigValueEscapes(t *testing.T) {
	if got, _ := ResolveMcpConfigValue("pa$$word"); got != "pa$word" {
		t.Fatalf("$$ = %q; want pa$word", got)
	}
	if got, _ := ResolveMcpConfigValue("$!bang"); got != "!bang" {
		t.Fatalf("$! = %q; want !bang", got)
	}
	if got, _ := ResolveMcpConfigValue(" !printf hi"); got != " !printf hi" {
		t.Fatalf("leading space should not run a command: %q", got)
	}
}

// runtime.ts keeps the last 2000 UTF-16 units of the JavaScript-trimmed
// stderr; slicing bytes split multi-byte characters.
func TestMcpStderrTailUsesJavaScriptStringUnits(t *testing.T) {
	tail := truncateMcpStderr(strings.Repeat("界", 2500))
	if got := ai.JSLength(tail); got != mcpStderrTailChars {
		t.Fatalf("tail units = %d; want %d", got, mcpStderrTailChars)
	}
	if !utf8.ValidString(tail) {
		t.Fatal("tail is not valid UTF-8; a byte slice split a character")
	}
	if got := truncateMcpStderr("\uFEFFerror"); got != "error" {
		t.Fatalf("tail = %q; want JavaScript trim to strip the BOM", got)
	}
}

// runtime.ts advertises the cwd with pathToFileURL(cwd).href, which escapes
// spaces and `#`, and stores JSTrim(instructions) or undefined.
func TestMcpConnectionAdvertisesRootsAsFileURLAndTrimsInstructions(t *testing.T) {
	fake, client := newMCPFakeServer(t, false)
	fake.instructions = "\uFEFFUse me."
	cwd := filepath.Join(t.TempDir(), "a b#c")
	connection := NewMcpServerConnection(McpServerConnectionOptions{
		Entry: McpServerEntry{Name: "fake", Config: &McpServerConfig{Command: "unused"}, Source: "test", Scope: McpScopeExtension},
		Cwd:   cwd,
		CreateTransport: func(McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error) {
			return client, nil
		},
		Credentials: NewMemoryMcpOAuthCredentialStore(),
	})
	t.Cleanup(func() { _ = connection.Close(context.Background()) })
	if _, err := connection.GetClient(context.Background()); err != nil {
		t.Fatal(err)
	}
	responses := make(chan *protocol.JsonRpcResponse, 1)
	fake.transport.OnMessage(func(message mcp.Message) {
		if response, ok := message.(*protocol.JsonRpcResponse); ok && response.ID.Num == 99 {
			responses <- response
		}
	})
	if err := fake.transport.Send(context.Background(), &protocol.JsonRpcRequest{
		JSONRPC: protocol.JSONRPCVersion, ID: protocol.NumberId(99), Method: "roots/list",
		Params: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	var response *protocol.JsonRpcResponse
	select {
	case response = <-responses:
	case <-time.After(2 * time.Second):
		t.Fatal("roots/list was not answered")
	}
	var listed struct {
		Roots []protocol.Root `json:"roots"`
	}
	if err := json.Unmarshal(response.Result, &listed); err != nil || len(listed.Roots) != 1 {
		t.Fatalf("roots = %s, err = %v", response.Result, err)
	}
	wantURI := (&url.URL{Scheme: "file", Path: cwd}).String()
	if listed.Roots[0].URI != wantURI {
		t.Fatalf("root uri = %s; want %s", listed.Roots[0].URI, wantURI)
	}
	if connection.Instructions == nil || *connection.Instructions != "Use me." {
		t.Fatalf("instructions = %v; want JavaScript-trimmed text", connection.Instructions)
	}
}
