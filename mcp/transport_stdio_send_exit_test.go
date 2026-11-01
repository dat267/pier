package mcp

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

// pi v1.4.0 (4a080252), packages/mcp/src/transports/stdio.ts send checks
// this.child?.stdin and throws McpConnectionClosedError after the child's close
// handler clears this.child, even if close() was never explicitly requested.
func TestStdioSendAfterServerExitReportsConnectionClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	transport := NewStdioTransport(StdioTransportOptions{
		Command: os.Args[0], Env: map[string]string{"PIER_MCP_UTF8_HELPER": "exit"},
	})
	t.Cleanup(func() { _ = transport.Close(context.Background()) })
	closed := make(chan struct{})
	transport.OnClose(func() { close(closed) })
	if err := transport.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("server did not exit:", ctx.Err())
	}

	message := &protocol.JsonRpcNotification{JSONRPC: "2.0", Method: "notifications/initialized"}
	var connectionClosed *protocol.McpConnectionClosedError
	if err := transport.Send(ctx, message); !errors.As(err, &connectionClosed) {
		t.Errorf("Send after server exit = %T: %v, want McpConnectionClosedError", err, err)
	}
	// Explicit shutdown is the matching control and must report the same error.
	if err := transport.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := transport.Send(ctx, message); !errors.As(err, &connectionClosed) {
		t.Fatalf("Send after explicit close = %T: %v, want McpConnectionClosedError", err, err)
	}
}
