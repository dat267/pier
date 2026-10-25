package mcp

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

// pi v1.4.0 transports/stdio.ts decodes each stdout line/stderr chunk and the
// stderr snapshot with Buffer.toString("utf8"). Distinct bad bytes are distinct
// replacement characters; unlike TextDecoder, Buffer preserves a leading BOM.
func TestStdioUTF8DecodingMatchesBuffer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	chunks := make(chan string, 8)
	transport := NewStdioTransport(StdioTransportOptions{
		Command: os.Args[0], Env: map[string]string{"PIER_MCP_UTF8_HELPER": "wait"},
		OnStderr: func(chunk string) { chunks <- chunk },
	})
	t.Cleanup(func() { _ = transport.Close(context.Background()) })
	closed := make(chan struct{})
	transport.OnClose(func() { close(closed) })
	messages := make(chan string, 1)
	transport.OnMessage(func(message Message) { messages <- message.(*protocol.JsonRpcNotification).Method })
	if err := transport.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case method := <-messages:
		if method != "\ufffd\ufffd" {
			t.Errorf("notification method=%q, want two replacement characters", method)
		}
	case <-ctx.Done():
		t.Fatal("stdout notification was not decoded:", ctx.Err())
	}
	var reported strings.Builder
	select {
	case chunk := <-chunks:
		reported.WriteString(chunk)
	case <-ctx.Done():
		t.Fatal("stderr chunk was not decoded:", ctx.Err())
	}
	if err := transport.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if got := transport.Stderr(); got != "\ufeff\ufffd\ufffd" {
		t.Errorf("stderr=%q, want preserved BOM and two replacement characters", got)
	}
	for len(chunks) > 0 {
		reported.WriteString(<-chunks)
	}
	if got := reported.String(); got != "\ufeff\ufffd\ufffd" {
		t.Errorf("onStderr=%q, want preserved BOM and two replacement characters", got)
	}
}
