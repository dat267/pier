package mcp

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

// pi v1.4.0 streamable-http.ts sleeps with setTimeout(ms), whose runtime clamps
// delays above 2^31-1 to 1ms. An SSE retry value just above that range must not
// become a multi-day wait in the Go transport.
func TestStreamableHTTPRetryDelayOverflowDoesNotBlockReconnect(t *testing.T) {
	var fixture *httpFixture
	var gets atomic.Int64
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets.Add(1)
			w.Header().Set("content-type", "text/event-stream")
			_, _ = io.WriteString(w, "retry: 2147483648\n\n")
			return
		}
		fixture.protocolHandler(w, r, nil)
	})
	defer fixture.close(t)

	transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: fixture.url})
	t.Cleanup(func() { _ = transport.Close(context.Background()) })
	ctx := context.Background()
	if err := transport.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := transport.Send(ctx, &protocol.JsonRpcNotification{
		JSONRPC: protocol.JSONRPCVersion,
		Method:  "notifications/initialized",
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.NewTimer(500 * time.Millisecond)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for gets.Load() < 2 {
		select {
		case <-deadline.C:
			t.Fatalf("GET stream did not reconnect after overflowing retry delay; requests=%d", gets.Load())
		case <-tick.C:
		}
	}
}
