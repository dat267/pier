package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

// pi v1.4.0 transports/streamable-http.ts dispatches POSTs with its lifetime
// controller.signal. close() aborts that controller, not just the GET stream.
func TestStreamableHTTPCloseCancelsInFlightPost(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		close(started)
		select {
		case <-request.Context().Done():
		case <-release:
		}
	}))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); close(release); server.Close() })
	transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: server.URL})
	if err := transport.Start(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- transport.Send(ctx, &protocol.JsonRpcRequest{
			JSONRPC: protocol.JSONRPCVersion, ID: protocol.NumberId(1), Method: "fixture/held",
		})
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("POST did not reach the fixture")
	}
	if err := transport.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("closed transport POST unexpectedly succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not cancel the in-flight POST")
	}
}
