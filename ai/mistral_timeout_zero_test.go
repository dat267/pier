package ai

import (
	ctxpkg "context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// pi v1.4.0 mistral-conversations.ts uses options?.timeoutMs ?? 60_000;
// explicit zero must immediately abort the response-header wait.
func TestMistralExplicitZeroTimeoutAbortsHeaderWait(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		select {
		case <-request.Context().Done():
		case <-time.After(20 * time.Millisecond):
		}
	}))
	defer server.Close()

	zero := 0
	ctx, cancel := ctxpkg.WithTimeout(ctxpkg.Background(), 100*time.Millisecond)
	defer cancel()
	model := &Model{BaseURL: server.URL}
	_, err := requestMistralStream(ctx, model, []byte(`{}`), &MistralOptions{
		StreamOptions: StreamOptions{APIKey: "test", TimeoutMs: &zero},
	})
	if err == nil || !strings.Contains(err.Error(), "Mistral response headers timed out after 0ms") {
		t.Fatalf("request error = %v, want immediate 0ms header timeout", err)
	}
}
