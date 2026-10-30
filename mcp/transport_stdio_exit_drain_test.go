package mcp

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

// pi v1.4.0 transports/stdio.ts reports a non-whitespace stdout tail as an
// incomplete JSON-RPC message before emitting the child-close notification.
func TestStdioExitReportsUnterminatedOutputBeforeClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	transport := NewStdioTransport(StdioTransportOptions{Command: os.Args[0], Env: map[string]string{"PIER_MCP_UTF8_HELPER": "partial"}})
	t.Cleanup(func() { _ = transport.Close(context.Background()) })
	events := make(chan string, 2)
	transport.OnError(func(err error) { events <- err.Error() })
	transport.OnClose(func() { events <- "close" })
	if err := transport.Start(ctx); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"MCP stdio server closed with an incomplete JSON-RPC message",
		"close",
	} {
		select {
		case got := <-events:
			if got != want {
				t.Fatalf("event = %q, want %q", got, want)
			}
		case <-ctx.Done():
			t.Fatalf("did not receive event %q: %v", want, ctx.Err())
		}
	}
}

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
