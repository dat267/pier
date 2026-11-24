package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dat267/pier/mcp/protocol"
	"sync"
	"testing"
	"time"
)

// Port of packages/mcp/test/client.test.ts: the in-memory pair plays the
// server, capturing every message and answering from per-method handlers.

type testServer struct {
	transport *InMemoryTransport
	mu        sync.Mutex
	messages  []Message
	handlers  map[string]func(*protocol.JsonRpcRequest) (any, error)
}

func newTestServer() (*testServer, *InMemoryTransport) {
	clientTransport, serverTransport := CreateInMemoryTransportPair()
	server := &testServer{transport: serverTransport, handlers: map[string]func(*protocol.JsonRpcRequest) (any, error){}}
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

// handleInitialize answers initialize with the latest protocol version, no
// capabilities and the test server's info.
func (s *testServer) handleInitialize() {
	s.setHandler("initialize", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{
			"protocolVersion": protocol.LatestProtocolVersion,
			"capabilities":    map[string]any{},
			"serverInfo":      map[string]any{"name": "test-server", "version": "1.0.0"},
		}, nil
	})
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

func TestClientRenewsTimeoutOnProgress(t *testing.T) {
	client, server := connectTestClient(t)
	const (
		progressInterval = 20 * time.Millisecond
		progressCount    = 10
		// The timeout has to clear one progress interval by a wide margin, so that
		// scheduler jitter cannot make the timeout win the race, and it has to sit well
		// below the handler's total time, so that a non-renewing timer still fails the
		// call. A 40ms notification against a 50ms timeout gave jitter ten milliseconds,
		// which lost under a full-suite load.
		requestTimeout = 100 * time.Millisecond
	)
	server.setHandler("tools/call", func(request *protocol.JsonRpcRequest) (any, error) {
		var params struct {
			Meta struct {
				ProgressToken int64 `json:"progressToken"`
			} `json:"_meta"`
		}
		_ = json.Unmarshal(request.Params, &params)
		token := params.Meta.ProgressToken
		for count := 1; count <= progressCount; count++ {
			time.Sleep(progressInterval)
			enc, _ := json.Marshal(map[string]any{"progressToken": token, "progress": count, "total": progressCount})
			_ = server.transport.Send(context.Background(), protocol.JsonRpcNotification{
				JSONRPC: protocol.JSONRPCVersion, Method: "notifications/progress", Params: enc,
			})
		}
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": "done"}}}, nil
	})
	var mu sync.Mutex
	var progress []*protocol.ProgressNotification
	done := make(chan error, 1)
	go func() {
		// Every notification renews the timeout, so the call outlives requestTimeout; a
		// regression to the non-renewing timer would fail it at requestTimeout.
		result, err := client.CallTool(context.Background(), "slow", map[string]any{}, RequestOptions{
			TimeoutMs: int64(requestTimeout / time.Millisecond),
			OnProgress: func(notification *protocol.ProgressNotification) {
				mu.Lock()
				progress = append(progress, notification)
				mu.Unlock()
			},
		})
		if err != nil {
			done <- err
			return
		}
		if len(result.Content) != 1 || result.Content[0].Text != "done" {
			done <- fmt.Errorf("result = %s", jsonNormalize(t, result))
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("slow call: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the call did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(progress) == 0 || progress[0].Progress != 1 || progress[0].Total == nil || *progress[0].Total != progressCount {
		t.Fatalf("progress = %s", jsonNormalize(t, progress))
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClientCancelsAbortedAndTimedOutRequests(t *testing.T) {
	client, server := connectTestClient(t)
	release := blockUntilCleanup(t)
	server.setHandler("tools/call", func(*protocol.JsonRpcRequest) (any, error) {
		<-release
		return nil, errors.New("test ended")
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
		if _, ok := errors.AsType[*protocol.McpAbortError](err); !ok {
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
		if _, ok := errors.AsType[*protocol.McpTimeoutError](err); !ok {
			t.Fatalf("timeout err = %v (%T)", err, err)
		}
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClientTimedOutInitializeSendsNoCancellation(t *testing.T) {
	server, clientTransport := newTestServer()
	release := blockUntilCleanup(t)
	server.setHandler("initialize", func(*protocol.JsonRpcRequest) (any, error) {
		<-release
		return nil, errors.New("test ended")
	})
	client := NewClient(ClientOptions{Name: "test-client", Version: "1.0.0", RequestTimeoutMs: 5})
	if _, err := client.Connect(context.Background(), clientTransport); err == nil {
		t.Fatal("connect did not time out")
	} else {
		if _, ok := errors.AsType[*protocol.McpTimeoutError](err); !ok {
			t.Fatalf("connect err = %v (%T)", err, err)
		}
	}
	for _, message := range server.recordedMessages() {
		if notification, ok := message.(*protocol.JsonRpcNotification); ok && notification.Method == "notifications/cancelled" {
			t.Fatalf("cancellation sent: %s", jsonNormalize(t, notification))
		}
	}
}

// client.ts spreads params then sets _meta.progressToken last, so the
// request's own token replaces a caller-supplied one while other _meta keys
// survive.
func TestClientReplacesCallerSuppliedProgressToken(t *testing.T) {
	client, server := connectTestClient(t)
	defer func() {
		if err := client.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()
	server.setHandler("probe", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{}, nil
	})
	if _, err := client.Request(context.Background(), "probe", map[string]any{
		"_meta": map[string]any{"progressToken": "caller-token", "trace": "abc"},
	}, RequestOptions{OnProgress: func(*protocol.ProgressNotification) {}}); err != nil {
		t.Fatal(err)
	}
	var call *protocol.JsonRpcRequest
	for _, message := range server.recordedMessages() {
		if request, ok := message.(*protocol.JsonRpcRequest); ok && request.Method == "probe" {
			call = request
		}
	}
	if call == nil {
		t.Fatal("no probe recorded")
	}
	var params struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(call.Params, &params); err != nil {
		t.Fatal(err)
	}
	var token float64
	if err := json.Unmarshal(params.Meta["progressToken"], &token); err != nil {
		t.Fatalf("progressToken = %s; want the numeric request id (%v)", params.Meta["progressToken"], err)
	}
	if token != call.ID.Num {
		t.Fatalf("progressToken = %v; want the request id %v", token, call.ID.Num)
	}
	var trace string
	if err := json.Unmarshal(params.Meta["trace"], &trace); err != nil || trace != "abc" {
		t.Fatalf("trace = %q, %v; want the caller's _meta keys preserved", trace, err)
	}
}

// client.ts handleCancelled narrows the id with isJsonRpcId; a numeric id
// decodes to float64 in Go, not json.Number.
func TestClientCancelsIncomingRequestByNumericID(t *testing.T) {
	client, server := connectTestClient(t)
	defer func() {
		if err := client.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()
	started := make(chan struct{})
	cancelled := make(chan struct{})
	client.SetRequestHandler("sampling/createMessage", func(ctx context.Context, _ json.RawMessage) (any, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	})
	go func() {
		_ = server.transport.Send(context.Background(), protocol.JsonRpcRequest{
			JSONRPC: protocol.JSONRPCVersion, ID: protocol.NumberId(42), Method: "sampling/createMessage",
			Params: json.RawMessage(`{}`),
		})
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the served request never started")
	}
	params, _ := json.Marshal(map[string]any{"requestId": 42, "reason": "user"})
	if err := server.transport.Send(context.Background(), protocol.JsonRpcNotification{
		JSONRPC: protocol.JSONRPCVersion, Method: "notifications/cancelled", Params: params,
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("a numeric requestId did not cancel the served request")
	}
}

// blockUntilCleanup returns a channel that closes when the test ends, for a
// handler that must never answer while the test runs but must not outlive it.
// Call it from the test, not the handler: a request can reach the handler after
// the test has finished, and a cleanup registered then never runs.
func blockUntilCleanup(t *testing.T) <-chan struct{} {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	return release
}

// Reference: pi v1.4.0 packages/mcp/src/client.ts handleMessage starts the async
// handleRequest without awaiting it. A handler awaiting cancellation must not
// block the stdio reader from dispatching notifications/cancelled.
func TestClientStdioHandlerCanReceiveCancellation(t *testing.T) {
	transport := NewStdioTransport(StdioTransportOptions{
		Command: stdioFixtureBin,
		Env:     map[string]string{"PIER_MCP_CANCEL_REQUEST_FIXTURE": "1"},
	})
	client := NewClient(ClientOptions{Name: "cancel-test", Version: "1"})
	defer client.Close(context.Background())
	entered, cancelled := make(chan struct{}), make(chan struct{})
	client.SetRequestHandler("wait", func(ctx context.Context, _ json.RawMessage) (any, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.Connect(ctx, transport); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("server request handler did not start")
	}
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("request handler blocked the stdio reader from dispatching cancellation")
	}
}

// pi v1.4.0 client.ts uses nullish fallback for request timeoutMs, so an
// explicit zero overrides the client default and disables the request timer.
func TestClientExplicitZeroTimeoutOverridesClientDefault(t *testing.T) {
	server, clientTransport := newTestServer()
	server.handleInitialize()
	release := make(chan struct{})
	server.setHandler("tools/call", func(*protocol.JsonRpcRequest) (any, error) {
		<-release
		return map[string]any{"content": []any{}}, nil
	})
	// Keep initialize's default timeout generous under the race detector; the
	// later caller deadline still distinguishes zero from this client default.
	client := NewClient(ClientOptions{Name: "test-client", Version: "1.0.0", RequestTimeoutMs: 100})
	if _, err := client.Connect(context.Background(), clientTransport); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		close(release)
		_ = client.Close(context.Background())
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := client.CallTool(ctx, "wait", nil, RequestOptions{TimeoutMs: 0, TimeoutMsSet: true})
	if _, ok := errors.AsType[*protocol.McpAbortError](err); !ok {
		t.Fatalf("explicit zero returned %v (%T), want caller-context abort", err, err)
	}
}

// pi v1.4.0 client.ts arms request timers with setTimeout for every finite,
// positive timeout, which clamps delays above 2^31-1 to 1ms.
func TestClientLargeRequestTimeoutUsesJavaScriptTimerClamp(t *testing.T) {
	for _, test := range []struct {
		name      string
		timeoutMs int64
	}{
		{name: "over_timer_range", timeoutMs: 1 << 31},
		{name: "over_safe_integer", timeoutMs: 1 << 54},
	} {
		t.Run(test.name, func(t *testing.T) {
			timeoutMs := test.timeoutMs
			server, clientTransport := newTestServer()
			server.handleInitialize()
			release := make(chan struct{})
			server.setHandler("tools/call", func(*protocol.JsonRpcRequest) (any, error) {
				<-release
				return map[string]any{"content": []any{}}, nil
			})
			client := NewClient(ClientOptions{Name: "test-client", Version: "1.0.0"})
			if _, err := client.Connect(context.Background(), clientTransport); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				close(release)
				_ = client.Close(context.Background())
			})

			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			_, err := client.CallTool(ctx, "wait", nil, RequestOptions{TimeoutMs: timeoutMs})
			cancel()
			if _, ok := errors.AsType[*protocol.McpTimeoutError](err); !ok {
				t.Fatalf("timeout %d returned %v (%T), want MCP timeout", timeoutMs, err, err)
			}
			if err := client.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// pi v1.4.0 client.ts uses ?? for requestTimeoutMs, so an explicit client-level
// zero disables the inherited request timer instead of selecting 30 seconds.
func TestClientExplicitZeroDefaultTimeoutDisablesTimer(t *testing.T) {
	server, clientTransport := newTestServer()
	server.handleInitialize()
	release := make(chan struct{})
	server.setHandler("tools/call", func(*protocol.JsonRpcRequest) (any, error) {
		<-release
		return map[string]any{"content": []any{}}, nil
	})
	client := NewClient(ClientOptions{Name: "test-client", Version: "1.0.0", RequestTimeoutMs: 0, RequestTimeoutMsSet: true})
	if _, err := client.Connect(context.Background(), clientTransport); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		close(release)
		_ = client.Close(context.Background())
	})
	go func() { _, _ = client.CallTool(context.Background(), "wait", nil, RequestOptions{}) }()

	deadline := time.Now().Add(time.Second)
	for {
		client.mu.Lock()
		var timeoutMs int64
		for _, pending := range client.pending {
			timeoutMs = pending.timeoutMs
			break
		}
		pending := len(client.pending) > 0
		client.mu.Unlock()
		if pending {
			if timeoutMs != 0 {
				t.Fatalf("pending request timeout = %dms, want explicit zero", timeoutMs)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("request was never pending")
		}
		time.Sleep(time.Millisecond)
	}
}

func (b *blockedUnknownReplyTransport) Send(ctx context.Context, message Message) error {
	if reply, ok := message.(protocol.JsonRpcResponse); ok && reply.Error != nil && reply.Error.Code == protocol.JSONRPCErrorCode.MethodNotFound {
		close(b.entered)
		select {
		case <-b.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return b.Transport.Send(ctx, message)
}

type initializedSendBarrier struct {
	Transport
	entered chan struct{}
	release chan struct{}
}

func (b *initializedSendBarrier) Send(ctx context.Context, message Message) error {
	err := b.Transport.Send(ctx, message)
	if notification, ok := message.(*protocol.JsonRpcNotification); ok && notification.Method == "notifications/initialized" && err == nil {
		close(b.entered)
		<-b.release // Dispatch already succeeded; hold only its completion.
	}
	return err
}
