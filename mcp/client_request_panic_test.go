package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

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
