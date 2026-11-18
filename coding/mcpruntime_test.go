package coding

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dat267/pier/mcp"
	"github.com/dat267/pier/mcp/protocol"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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

// pi v1.4.0 runtime.ts getClient shares one in-flight connect that is aborted
// only by close(); a caller's own signal does not drive it. Two concurrent
// first calls therefore both connect when one caller cancels.
func TestMcpConcurrentConnectSurvivesInitiatorCancellation(t *testing.T) {
	clientTransport, serverTransport := mcp.CreateInMemoryTransportPair()
	initializeStarted := make(chan struct{})
	releaseInitialize := make(chan struct{})
	var startedOnce sync.Once
	serverTransport.OnMessage(func(message mcp.Message) {
		request, ok := message.(*protocol.JsonRpcRequest)
		if !ok {
			return
		}
		var result any
		var failure *protocol.McpError
		switch request.Method {
		case "initialize":
			startedOnce.Do(func() { close(initializeStarted) })
			<-releaseInitialize
			result = map[string]any{
				"protocolVersion": protocol.LatestProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "fake", "version": "1.0.0"},
			}
		case "tools/list":
			result = map[string]any{"tools": []any{}}
		default:
			failure = &protocol.McpError{Code: protocol.JSONRPCErrorCode.MethodNotFound, Msg: "Method not found"}
		}
		response := protocol.JsonRpcResponse{JSONRPC: protocol.JSONRPCVersion, ID: request.ID}
		if failure != nil {
			response.Error = &protocol.JsonRpcErrorObject{Code: failure.Code, Message: failure.Msg}
		} else {
			encoded, err := json.Marshal(result)
			if err != nil {
				return
			}
			response.Result = encoded
		}
		_ = serverTransport.Send(context.Background(), response)
	})
	if err := serverTransport.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	connection := NewMcpServerConnection(McpServerConnectionOptions{
		Entry: McpServerEntry{Name: "slow", Config: &McpServerConfig{Command: "unused"}},
		CreateTransport: func(McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error) {
			return clientTransport, nil
		},
		Credentials: NewMemoryMcpOAuthCredentialStore(),
	})
	t.Cleanup(func() { _ = connection.Close(context.Background()) })

	initiatorCtx, cancelInitiator := context.WithCancel(context.Background())
	initiatorDone := make(chan error, 1)
	go func() {
		_, err := connection.GetClient(initiatorCtx)
		initiatorDone <- err
	}()
	select {
	case <-initializeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("connect never reached initialize")
	}

	waiterDone := make(chan error, 1)
	go func() {
		_, err := connection.GetClient(context.Background())
		waiterDone <- err
	}()
	// The waiter has joined the in-flight connect.
	deadline := time.Now().Add(2 * time.Second)
	for {
		connection.mu.Lock()
		waiting := connection.opening != nil
		connection.mu.Unlock()
		if waiting || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}

	cancelInitiator()
	close(releaseInitialize)
	select {
	case <-initiatorDone:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled initiator did not return")
	}
	select {
	case err := <-waiterDone:
		if err != nil {
			t.Fatalf("concurrent caller failed because another caller cancelled: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent caller did not settle")
	}
	connection.mu.Lock()
	state := connection.State
	connection.mu.Unlock()
	if state != McpServerConnected {
		t.Fatalf("state after a concurrent connect = %q, want connected", state)
	}
}

// The connection-scoped connect outlives an initiator that already returned on
// its own context; Close must still abort it and wait for it (CR-097 follow-up).
func TestMcpConnectionCloseAbortsConnectAfterInitiatorLeft(t *testing.T) {
	clientTransport, serverTransport := mcp.CreateInMemoryTransportPair()
	initializeStarted := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	var startedOnce sync.Once
	serverTransport.OnMessage(func(message mcp.Message) {
		request, ok := message.(*protocol.JsonRpcRequest)
		if ok && request.Method == "initialize" {
			startedOnce.Do(func() { close(initializeStarted) })
			<-release
		}
	})
	if err := serverTransport.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	connection := NewMcpServerConnection(McpServerConnectionOptions{
		Entry: McpServerEntry{Name: "slow", Config: &McpServerConfig{Command: "unused"}},
		CreateTransport: func(McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error) {
			return clientTransport, nil
		},
		Credentials: NewMemoryMcpOAuthCredentialStore(),
	})
	t.Cleanup(func() { _ = connection.Close(context.Background()) })

	initiatorCtx, cancelInitiator := context.WithCancel(context.Background())
	initiatorDone := make(chan error, 1)
	go func() {
		_, err := connection.GetClient(initiatorCtx)
		initiatorDone <- err
	}()
	select {
	case <-initializeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("connect never reached initialize")
	}
	cancelInitiator()
	select {
	case <-initiatorDone:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled initiator did not return")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- connection.Close(context.Background()) }()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not abort the abandoned connect")
	}
	connection.mu.Lock()
	state := connection.State
	opening := connection.opening
	connection.mu.Unlock()
	if state != McpServerClosed || opening != nil {
		t.Fatalf("state=%q opening=%v after Close", state, opening)
	}
}

// pi v1.4.0 (4a080252), extensions/mcp/runtime.ts getClient returns the shared
// open promise's result, not whatever client remains attached when a waiter
// resumes. A server can drop after publication but before open returns.
func TestMcpConnectRetainsItsResultAfterTransportDrop(t *testing.T) {
	server, transport := newMCPFakeServer(t, false)
	defer server.transport.Close(context.Background())
	connection := NewMcpServerConnection(McpServerConnectionOptions{
		Entry: McpServerEntry{Name: "fake", Config: &McpServerConfig{Command: "unused"}},
		CreateTransport: func(McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error) {
			return transport, nil
		},
		OnTools: func(*McpServerConnection) {
			// Drop the actual transport at this publication boundary; do not
			// mutate private connection fields to manufacture the interleaving.
			_ = server.transport.Close(context.Background())
		},
	})
	defer connection.Close(context.Background())
	client, err := connection.GetClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if client == nil {
		t.Fatal("a completed connect returned (nil, nil) after the transport dropped")
	}
	if client.ConnectionState() != mcp.StateClosed {
		t.Fatalf("dropped client state = %s, want closed", client.ConnectionState())
	}
}
