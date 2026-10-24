package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

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
