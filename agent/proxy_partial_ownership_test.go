package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), agent-loop.ts emits a message while the JS event
// loop still owns the provider partial. In Go, the provider continues on a
// goroutine: the emitted message and its inner event must own their block
// slots rather than aliasing that live helper (D285).
func TestAgentOwnsProxyPartialsDuringStreaming(t *testing.T) {
	ready := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"start\"}\n\ndata: {\"type\":\"text_start\",\"contentIndex\":0}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-ready:
		case <-r.Context().Done():
			return
		}
		for range 64 {
			_, _ = fmt.Fprint(w, "data: {\"type\":\"text_delta\",\"contentIndex\":0,\"delta\":\"x\"}\n\n")
			w.(http.Flusher).Flush()
		}
		_, _ = io.WriteString(w, "data: {\"type\":\"done\",\"reason\":\"stop\"}\n\n")
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	model := &ai.Model{ID: "m", Provider: "p", API: ai.APIOpenAIResponses}
	var stream *ai.AssistantMessageEventStream
	a, err := NewAgent(&AgentOptions{
		InitialState: &AgentInitialState{Model: model},
		StreamFn: func(m *ai.Model, c ai.TranscriptContext, o *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			stream = StreamProxy(m, c, ProxyStreamOptions{SimpleStreamOptions: *o, ProxyURL: server.URL})
			return stream
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	signaled := false
	a.Subscribe(func(event AgentEvent, _ context.Context) error {
		if event.Type == MessageUpdate && !signaled {
			signaled = true
			close(ready)
			// Let the producer finish while the listener retains this start
			// event. Result's publication orders the later reads, making
			// snapshot corruption observable even without the race detector.
			final, err := stream.Result(ctx)
			if err != nil {
				return err
			}
			if got := final.Content[0].(ai.TextContent).Text; got != strings.Repeat("x", 64) {
				t.Errorf("final text = %q", got)
			}
			for _, partial := range []*ai.AssistantMessage{event.Message.(*ai.AssistantMessage), event.AssistantMessageEvent.Partial} {
				if got := partial.Content[0].(ai.TextContent).Text; got != "" {
					t.Errorf("start event was mutated by later deltas: %q", got)
				}
			}
		}
		return nil
	})
	if err := a.PromptText(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	if !signaled {
		t.Fatal("no streaming update was delivered")
	}
}
