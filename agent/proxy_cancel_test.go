package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), agent/src/proxy.ts catch selects "aborted" when
// options.signal.aborted, including a cancellation while reader.read waits.
func TestStreamProxyCancelDuringBodyReadReportsAborted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"start\"}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := &ai.Model{ID: "m", Provider: "p", API: ai.APIOpenAIResponses}
	stream := StreamProxy(model, proxyContext(), ProxyStreamOptions{
		ProxyURL:            server.URL,
		SimpleStreamOptions: ai.SimpleStreamOptions{StreamOptions: ai.StreamOptions{Ctx: ctx}},
	})
	waitCtx, stopWait := context.WithTimeout(context.Background(), time.Second)
	defer stopWait()
	event, ok := stream.Next(waitCtx)
	if !ok || event.Type != ai.EventStart {
		t.Fatal("proxy did not publish start before cancellation")
	}
	cancel()
	result, err := stream.Result(waitCtx)
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != ai.StopAborted || result.ErrorMessage == nil || *result.ErrorMessage != "Request aborted by user" {
		t.Fatalf("response = %+v; want aborted with the proxy's cancellation message", result)
	}
}
