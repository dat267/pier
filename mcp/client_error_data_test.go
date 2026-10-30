package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

// Reference: pi v1.4.0 packages/mcp/src/client.ts handleRequest copies
// McpError.data into the JSON-RPC error response without restricting its type.
func TestClientServedRequestPreservesStructuredErrorData(t *testing.T) {
	client, server := connectTestClient(t)
	defer func() {
		if err := client.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	client.SetRequestHandler("test/structured-error", func(context.Context, json.RawMessage) (any, error) {
		return nil, &protocol.McpError{
			Code: 1234,
			Msg:  "structured failure",
			Data: map[string]any{"retryable": false, "attempt": 2},
		}
	})
	responses := make(chan *protocol.JsonRpcResponse, 1)
	server.transport.OnMessage(func(message Message) {
		if response, ok := message.(*protocol.JsonRpcResponse); ok && response.ID == protocol.StringId("incoming") {
			responses <- response
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.transport.Send(ctx, protocol.JsonRpcRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      protocol.StringId("incoming"),
		Method:  "test/structured-error",
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case response := <-responses:
		if response.Error == nil {
			t.Fatalf("response = %+v, want an error", response)
		}
		var data map[string]any
		if err := json.Unmarshal(response.Error.Data, &data); err != nil {
			t.Fatalf("error data = %s, %v", response.Error.Data, err)
		}
		if data["retryable"] != false || data["attempt"] != float64(2) {
			t.Fatalf("error data = %#v", data)
		}
	case <-ctx.Done():
		t.Fatal("no response to served request")
	}
}

// Reference: pi v1.4.0 packages/mcp/src/client.ts handleRequest normalizes a
// null handler result to an empty object with `result ?? {}`.
func TestClientServedRequestNormalizesTypedNilResult(t *testing.T) {
	client, server := connectTestClient(t)
	defer func() {
		if err := client.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	client.SetRequestHandler("test/empty-result", func(context.Context, json.RawMessage) (any, error) {
		var result map[string]any
		return result, nil
	})
	responses := make(chan *protocol.JsonRpcResponse, 1)
	server.transport.OnMessage(func(message Message) {
		if response, ok := message.(*protocol.JsonRpcResponse); ok && response.ID == protocol.StringId("empty") {
			responses <- response
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.transport.Send(ctx, protocol.JsonRpcRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      protocol.StringId("empty"),
		Method:  "test/empty-result",
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case response := <-responses:
		if string(response.Result) != `{}` {
			t.Fatalf("result = %s, want {}", response.Result)
		}
	case <-ctx.Done():
		t.Fatal("no response to served request")
	}
}
