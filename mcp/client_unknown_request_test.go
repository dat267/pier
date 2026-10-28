package mcp

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

type blockedUnknownReplyTransport struct {
	Transport
	entered, release chan struct{}
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
