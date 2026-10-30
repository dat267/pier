package ai

import (
	ctxpkg "context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// pi v1.4.0 mistral-conversations.ts uses options?.timeoutMs ?? 60_000;
// explicit zero must immediately abort the response-header wait.
func TestMistralAbortSignalTimeoutRejectsOutOfRangeDelays(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	for _, timeoutMS := range []int{-1, 4_294_967_296} {
		t.Run(strconv.Itoa(timeoutMS), func(t *testing.T) {
			requests.Store(0)
			response, err := requestMistralStream(ctxpkg.Background(), &Model{BaseURL: server.URL}, []byte(`{}`), &MistralOptions{
				StreamOptions: StreamOptions{APIKey: "test", TimeoutMs: &timeoutMS},
			})
			if response != nil {
				response.Body.Close()
			}
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "out of range") {
				t.Fatalf("request error = %v, want timeout range error", err)
			}
			if got := requests.Load(); got != 0 {
				t.Fatalf("fetch requests = %d, want validation before fetch", got)
			}
		})
	}
}

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
