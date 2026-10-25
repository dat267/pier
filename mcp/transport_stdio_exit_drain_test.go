package mcp

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

// pi v1.4.0 transports/stdio.ts finalizes on ChildProcess.close, after stdout
// and stderr have drained, not on process exit. A listener may initiate close
// without preventing the remaining pipe data or the final close notification.
func TestStdioExitDrainsOutputBeforeCloseEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	transport := NewStdioTransport(StdioTransportOptions{Command: os.Args[0], Env: map[string]string{"PIER_MCP_UTF8_HELPER": "exit"}})
	t.Cleanup(func() { _ = transport.Close(context.Background()) })
	messages := make(chan string, 1)
	transport.OnMessage(func(message Message) {
		if err := transport.Close(ctx); err != nil {
			messages <- "close error: " + err.Error()
			return
		}
		messages <- message.(*protocol.JsonRpcNotification).Method
	})
	closed := make(chan struct{})
	transport.OnClose(func() { close(closed) })
	if err := transport.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("close notification did not arrive after reentrant Close:", ctx.Err())
	}
	select {
	case method := <-messages:
		if method != "\ufffd\ufffd" {
			t.Errorf("method=%q, want the complete final notification", method)
		}
	default:
		t.Error("OnClose fired before the final notification completed")
	}
	if got := transport.Stderr(); got != "\ufeff\ufffd\ufffd" {
		t.Errorf("OnClose stderr=%q, want complete final stderr", got)
	}
}
