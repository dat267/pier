package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 packages/agent/src/proxy.ts buildProxyRequestOptions forwards
// ProviderHeaders unchanged, including null values that suppress defaults at
// the remote provider. Dropping a null header silently restores that default.
func TestStreamProxyPreservesNullHeaderOverrides(t *testing.T) {
	bodies := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		bodies <- body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"done\",\"reason\":\"stop\"}\n\n")
	}))
	defer server.Close()
	value := "fixture"
	model := &ai.Model{ID: "m", Provider: "p", API: ai.APIOpenAIResponses}
	_, err := StreamProxy(model, proxyContext(), ProxyStreamOptions{
		ProxyURL: server.URL,
		SimpleStreamOptions: ai.SimpleStreamOptions{StreamOptions: ai.StreamOptions{
			Headers: ai.ProviderHeaders{"X-Remove": nil, "X-Send": &value},
		}},
	}).Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Options struct {
			Headers map[string]json.RawMessage
		}
	}
	if err := json.Unmarshal(<-bodies, &request); err != nil {
		t.Fatal(err)
	}
	if got := string(request.Options.Headers["X-Remove"]); got != "null" {
		t.Fatalf("suppressed header = %q, want explicit JSON null", got)
	}
	if got := string(request.Options.Headers["X-Send"]); got != `"fixture"` {
		t.Fatalf("ordinary header = %q, want fixture", got)
	}
}
