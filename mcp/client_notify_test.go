package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dat267/pier/mcp/protocol"
	"sync"
	"testing"
	"time"
)

// Port of packages/mcp/test/client.test.ts: the in-memory pair plays the
// server, capturing every message and answering from per-method handlers.

func TestClientNotifiesCloseListenersOnceOnTransportDrop(t *testing.T) {
	client, server := connectTestClient(t)
	closeCount := 0
	client.OnClose(func() { closeCount++ })
	release := blockUntilCleanup(t)
	server.setHandler("tools/call", func(*protocol.JsonRpcRequest) (any, error) {
		<-release
		return nil, errors.New("test ended")
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
		Roots:   []protocol.Root{{URI: "file:///workspace", Name: new("workspace")}},
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

// client.ts looks a response up by its exact JSON-RPC id, so a fractional id
// must not resolve the integer request it rounds down to.
func TestClientIgnoresResponseWithNonIntegerID(t *testing.T) {
	client, server := connectTestClient(t)
	defer func() {
		if err := client.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()
	release := make(chan struct{})
	started := make(chan protocol.JsonRpcId, 1)
	server.setHandler("tools/call", func(request *protocol.JsonRpcRequest) (any, error) {
		started <- request.ID
		<-release
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": "real"}}}, nil
	})
	reported := make(chan error, 8)
	client.OnError(func(err error) { reported <- err })
	type outcome struct {
		result *protocol.CallToolResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := client.CallTool(context.Background(), "meta", nil, RequestOptions{})
		done <- outcome{result: result, err: err}
	}()
	var id protocol.JsonRpcId
	select {
	case id = <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the tool call never reached the server")
	}
	if err := server.transport.Send(context.Background(), protocol.JsonRpcResponse{
		JSONRPC: protocol.JSONRPCVersion, ID: protocol.NumberId(id.Num + 0.5),
		Result: json.RawMessage(`{"content":[{"type":"text","text":"bogus"}]}`),
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reported:
	case outcome := <-done:
		t.Fatalf("a fractional id resolved request %v: %+v", id, outcome)
	case <-time.After(2 * time.Second):
		t.Fatal("the fractional response was not dispatched")
	}
	close(release)
	select {
	case outcome := <-done:
		if outcome.err != nil {
			t.Fatal(outcome.err)
		}
		if len(outcome.result.Content) != 1 || outcome.result.Content[0].Text != "real" {
			t.Fatalf("result = %s", jsonNormalize(t, outcome.result))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the real response did not resolve the call")
	}
}

// D312: pi v1.4.0 packages/mcp/src/client.ts iterates its live Set, so a
// listener removed by an earlier callback is skipped for the current message.
// Go's lock-safe snapshot intentionally keeps already-admitted listeners.
func TestClientNotificationUnsubscribeDuringDispatchUsesSnapshot(t *testing.T) {
	client := NewClient(ClientOptions{})
	var got []string
	var removeSecond func()
	client.OnNotification("test/event", func(json.RawMessage) {
		got = append(got, "first")
		removeSecond()
	})
	removeSecond = client.OnNotification("test/event", func(json.RawMessage) {
		got = append(got, "second")
	})

	client.handleNotification(&protocol.JsonRpcNotification{Method: "test/event"})
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("listeners called = %v, want [first second] for the admitted snapshot", got)
	}
}

// Reference: pi v1.4.0 packages/mcp/src/client.ts handleRequest catches an
// exception from the registered handler and sends a JSON-RPC error response.
func TestClientServerRequestHandlerPanicBecomesRPCError(t *testing.T) {
	client, server := connectTestClient(t)
	defer client.Close(context.Background())
	client.SetRequestHandler("explode", func(context.Context, json.RawMessage) (any, error) {
		panic(errors.New("handler failed"))
	})
	response := make(chan *protocol.JsonRpcResponse, 1)
	server.transport.OnMessage(func(message Message) {
		if reply, ok := message.(*protocol.JsonRpcResponse); ok && reply.ID == protocol.StringId("panic-request") {
			response <- reply
		}
	})
	if err := server.transport.Send(context.Background(), protocol.JsonRpcRequest{
		JSONRPC: protocol.JSONRPCVersion, ID: protocol.StringId("panic-request"), Method: "explode",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case reply := <-response:
		if reply.Error == nil || reply.Error.Code != protocol.JSONRPCErrorCode.InternalError || reply.Error.Message != "handler failed" {
			t.Fatalf("panic response = %+v, want internal error handler failed", reply)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler panic produced no JSON-RPC error response")
	}
}

type blockedUnknownReplyTransport struct {
	Transport
	entered, release chan struct{}
}

// pi v1.4.0 (4a080252), mcp/src/client.ts handleMessage launches the async
// handleRequest without awaiting it, including an unknown method's error reply.
// A blocked stdio write must not stall the reader's unrelated responses.
func TestClientUnknownRequestReplyDoesNotBlockResponses(t *testing.T) {
	server, transport := newTestServer()
	server.setHandler("initialize", func(*protocol.JsonRpcRequest) (any, error) {
		return map[string]any{
			"protocolVersion": protocol.LatestProtocolVersion,
			"capabilities":    map[string]any{},
			"serverInfo":      map[string]any{"name": "test", "version": "1"},
		}, nil
	})
	server.setHandler("ping", func(*protocol.JsonRpcRequest) (any, error) { return map[string]any{}, nil })
	blocked := &blockedUnknownReplyTransport{Transport: transport, entered: make(chan struct{}), release: make(chan struct{})}
	client := NewClient(ClientOptions{Name: "test", Version: "1"})
	defer client.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := client.Connect(ctx, blocked); err != nil {
		t.Fatal(err)
	}
	unblock := sync.OnceFunc(func() { close(blocked.release) })
	defer unblock()
	replies := make(chan *protocol.JsonRpcResponse, 1)
	server.transport.OnMessage(func(message Message) {
		if reply, ok := message.(*protocol.JsonRpcResponse); ok && reply.ID == protocol.StringId("unknown") {
			replies <- reply
		}
	})
	if err := server.transport.Send(ctx, protocol.JsonRpcRequest{
		JSONRPC: protocol.JSONRPCVersion, ID: protocol.StringId("unknown"), Method: "unregistered",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blocked.entered:
	case <-ctx.Done():
		t.Fatal("unknown request reply did not reach the blocked writer")
	}
	if err := client.Ping(ctx, RequestOptions{}); err != nil {
		t.Fatalf("blocked unknown-method reply stalled an unrelated response: %v", err)
	}
	unblock()
	select {
	case reply := <-replies:
		if reply.Error == nil || reply.Error.Code != protocol.JSONRPCErrorCode.MethodNotFound || reply.Error.Message != "Method not found: unregistered" {
			t.Fatalf("unknown-method reply = %+v", reply)
		}
	case <-ctx.Done():
		t.Fatal("unknown-method reply was not delivered after output recovered")
	}
}

// D302: pi v1.4.0 (4a080252), packages/mcp/src/client.ts connect() also
// assigns connected unconditionally after await notifyInternal. Pier preserves
// the terminal closed state if Close or a transport drop won in that interval.
func TestClientConnectDoesNotResurrectClosedConnection(t *testing.T) {
	for _, mode := range []string{"control", "client close", "peer drop"} {
		t.Run(mode, func(t *testing.T) {
			server, transport := newTestServer()
			server.setHandler("initialize", func(*protocol.JsonRpcRequest) (any, error) {
				return map[string]any{
					"protocolVersion": protocol.LatestProtocolVersion,
					"capabilities":    map[string]any{},
					"serverInfo":      map[string]any{"name": "fixture", "version": "1"},
				}, nil
			})
			barrier := &initializedSendBarrier{Transport: transport, entered: make(chan struct{}), release: make(chan struct{})}
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(barrier.release) }) }
			defer release()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client := NewClient(ClientOptions{Name: "fixture", Version: "1"})
			defer client.Close(context.Background())
			done := make(chan error, 1)
			go func() { _, err := client.Connect(ctx, barrier); done <- err }()
			select {
			case <-barrier.entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			switch mode {
			case "client close":
				if err := client.Close(ctx); err != nil {
					t.Fatal(err)
				}
			case "peer drop":
				if err := server.transport.Close(ctx); err != nil {
					t.Fatal(err)
				}
			}
			release()
			var err error
			select {
			case err = <-done:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if mode == "control" {
				if err != nil || client.ConnectionState() != StateConnected {
					t.Fatalf("control connect err=%v state=%s", err, client.ConnectionState())
				}
				return
			}
			var closed *protocol.McpConnectionClosedError
			if !errors.As(err, &closed) || client.ConnectionState() != StateClosed {
				t.Fatalf("closed client resurrected: connect err=%v state=%s", err, client.ConnectionState())
			}
		})
	}
}

// pi v1.5.2 (b4afdddf) packages/mcp/src/protocol/jsonrpc.ts toError wraps a
// non-Error with `new Error(String(value))`, and client.ts calls it on whatever a
// notification listener throws. A thrown object reports "[object Object]" there,
// an array joins its items with commas, and null reports "null"; the Go port
// formatted the recovered value with %v and reported "map[...]", "[a b]" and
// "<nil>". A notification listener is public API (`OnNotification`), so a
// consumer's panic reaches this.
func TestToErrorStringifiesLikeJavaScript(t *testing.T) {
	client, server := connectTestClient(t)
	reported := make(chan error, 1)
	client.OnError(func(err error) {
		select {
		case reported <- err:
		default:
		}
	})
	client.OnNotification("probe", func(json.RawMessage) {
		panic(map[string]any{"code": "ENOENT"})
	})
	if err := server.transport.Send(context.Background(), protocol.JsonRpcNotification{JSONRPC: "2.0", Method: "probe"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-reported:
		if err.Error() != "[object Object]" {
			t.Fatalf("reported %q, want %q", err.Error(), "[object Object]")
		}
	case <-time.After(time.Second):
		t.Fatal("no error reported")
	}
}
