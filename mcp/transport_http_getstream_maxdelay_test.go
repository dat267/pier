package mcp

import (
	"context"
	"math"
	"net/http"
	"sync"
	"testing"
	"time"
)

// pi v1.5.0 streamable-http.ts runGetStream compares elapsed milliseconds with
// maxDelayMs. A huge maxDelay must keep the retry budget intact, not overflow a
// time.Duration into a negative "healthy" threshold.
func TestStreamableHTTPLargeMaxDelayDoesNotResetRetryBudget(t *testing.T) {
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
	})
	defer fixture.close(t)

	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	dropped := make(chan error, 1)
	client.OnError(func(err error) { dropped <- err })
	if _, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{
		URL: fixture.url,
		Reconnect: &StreamableHttpReconnectOptions{
			InitialDelayMs: 1, MaxDelayMs: math.MaxInt64, MaxDelayMsSet: true, MaxRetries: 2,
		},
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-dropped:
		if err.Error() != "MCP server-to-client stream dropped and could not be reopened" {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("large maxDelay prevented retry exhaustion")
	}
	mu.Lock()
	attempts := gets
	mu.Unlock()
	if attempts != 3 {
		t.Fatalf("GET attempts = %d, want 3", attempts)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
