package mcp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

type trackedSseBody struct {
	io.Reader
	closed chan struct{}
}

func (b *trackedSseBody) Close() error {
	close(b.closed)
	return nil
}

// Reference: pi v1.3.0 packages/mcp/src/transports/streamable-http.ts releases
// its SSE reader in finally. The Go HTTP body must likewise be closed when
// consumption finishes, so its connection and request resources are released.
func TestHttpResponseStreamClosesBodyAfterEOF(t *testing.T) {
	body := &trackedSseBody{
		Reader: strings.NewReader("data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n"),
		closed: make(chan struct{}),
	}
	transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{
		URL: "https://mcp.example.test",
		Fetch: func(context.Context, *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}, nil
		},
	})
	defer transport.Close(context.Background())
	if err := transport.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := transport.Send(context.Background(), &protocol.JsonRpcRequest{
		JSONRPC: protocol.JSONRPCVersion, ID: protocol.NumberId(1), Method: "ping",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-body.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("completed SSE response body was never closed")
	}
}
