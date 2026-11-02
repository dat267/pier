package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// M35: an MCP server is untrusted network input. A successful JSON-RPC
// response over the Streamable HTTP transport must respect the same per-message
// cap the stdio transport and SSE stream already enforce.
func TestStreamableHTTPRejectsOversizedJSONResponse(t *testing.T) {
	fixture := newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var request struct {
			ID json.RawMessage `json:"id"`
		}
		_ = json.Unmarshal(body, &request)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":`))
		_, _ = w.Write(request.ID)
		_, _ = w.Write([]byte(`,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"x","version":"1"},"padding":"`))
		_, _ = w.Write([]byte(strings.Repeat("x", DefaultMaxMessageBytes+1024)))
		_, _ = w.Write([]byte(`"}}`))
	})
	defer fixture.close(t)
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	if _, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: fixture.url})); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized JSON response accepted: err=%v", err)
	}
}
