package mcp

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

// pi v1.4.0 (4a080252), packages/mcp/src/transports/streamable-http.ts
// runGetStream: the "stayed up for a while counts as healthy" reset runs only
// after consumeSse resolves. A stream that breaks mid-flight skips it, so the
// attempt counter reaches maxRetries and the drop is reported instead of
// reconnecting forever.
func TestStreamableHTTPAbortedGetStreamsExhaustRetries(t *testing.T) {
	var mu sync.Mutex
	gets := 0
	var fixture *httpFixture
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			fixture.protocolHandler(w, r, nil)
			return
		}
		mu.Lock()
		gets++
		mu.Unlock()
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, "id: g1\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/tools/list_changed\"}\n\n")
		w.(http.Flusher).Flush()
		// Stay open past maxDelayMs so a completed stream would count as
		// healthy, then abort mid-chunk: the reader sees an error, not EOF.
		time.Sleep(25 * time.Millisecond)
		panic(http.ErrAbortHandler)
	})
	defer fixture.close(t)

	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	dropped := make(chan error, 16)
	client.OnError(func(err error) { dropped <- err })
	if _, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{
		URL: fixture.url,
		Reconnect: &StreamableHttpReconnectOptions{
			InitialDelayMs: 1, MaxDelayMs: 10, MaxDelayMsSet: true, MaxRetries: 2,
		},
	})); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-dropped:
		if err.Error() != "MCP server-to-client stream dropped and could not be reopened" {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("aborted GET streams never exhausted their retries")
	}
	mu.Lock()
	attempts := gets
	mu.Unlock()
	// Three failed attempts (the initial one plus MaxRetries) then give up.
	if attempts != 3 {
		t.Fatalf("GET attempts = %d, want 3", attempts)
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	after := gets
	mu.Unlock()
	if after != attempts {
		t.Fatalf("GET attempts kept growing after the drop was reported: %d -> %d", attempts, after)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
