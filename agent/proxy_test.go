package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

func TestProxyTextDeltaAssemblyStaysLinearInAllocatedBytes(t *testing.T) {
	const chunks = 512
	const chunkSize = 128
	delta := strings.Repeat("x", chunkSize)
	got, allocated := measureProxyDeltaAssembly(t, "text_start", "text_delta", ai.TextContent{}, delta, chunks)
	assertProxyDeltaAssembly(t, got, allocated, delta, chunks)
}

func TestProxyThinkingDeltaAssemblyStaysLinearInAllocatedBytes(t *testing.T) {
	const chunks = 512
	const chunkSize = 128
	delta := strings.Repeat("x", chunkSize)
	got, allocated := measureProxyDeltaAssembly(t, "thinking_start", "thinking_delta", ai.ThinkingContent{}, delta, chunks)
	assertProxyDeltaAssembly(t, got, allocated, delta, chunks)
}

func measureProxyDeltaAssembly(t *testing.T, startType, deltaType string, initial ai.Content, delta string, chunks int) (string, uint64) {
	t.Helper()
	partial := &ai.AssistantMessage{Content: ai.ContentList{initial}}
	scratch := newProxyEventScratch()
	_, _ = processProxyEvent(&proxyWireEvent{Type: startType}, partial, scratch)

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range chunks {
		_, _ = processProxyEvent(&proxyWireEvent{Type: deltaType, Delta: delta}, partial, scratch)
	}
	runtime.ReadMemStats(&after)

	switch content := partial.Content[0].(type) {
	case ai.TextContent:
		return content.Text, after.TotalAlloc - before.TotalAlloc
	case ai.ThinkingContent:
		return content.Thinking, after.TotalAlloc - before.TotalAlloc
	default:
		t.Fatalf("content = %T, want text or thinking", content)
		return "", 0
	}
}

func assertProxyDeltaAssembly(t *testing.T, got string, allocated uint64, delta string, chunks int) {
	t.Helper()
	if want := strings.Repeat(delta, chunks); got != want {
		t.Fatalf("assembled content length = %d, want %d", len(got), len(want))
	}
	outputBytes := uint64(chunks * len(delta))
	if allocated > outputBytes*8 {
		t.Fatalf("assembling %d bytes allocated %d bytes, want at most %d", outputBytes, allocated, outputBytes*8)
	}
}

func BenchmarkProxyTextDeltaAssembly(b *testing.B) {
	const chunks = 512
	delta := strings.Repeat("x", 128)
	b.ReportAllocs()
	for b.Loop() {
		partial := &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{}}}
		scratch := newProxyEventScratch()
		_, _ = processProxyEvent(&proxyWireEvent{Type: "text_start"}, partial, scratch)
		for range chunks {
			_, _ = processProxyEvent(&proxyWireEvent{Type: "text_delta", Delta: delta}, partial, scratch)
		}
		if got := partial.Content[0].(ai.TextContent).Text; len(got) != chunks*len(delta) {
			b.Fatalf("assembled content length = %d, want %d", len(got), chunks*len(delta))
		}
	}
}

func proxyContext() ai.TranscriptContext {
	return ai.TranscriptContext{Messages: []ai.Message{
		&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}, Timestamp: 1},
	}}
}

// pi v1.3.0 agent/src/proxy.ts uses JS array properties; a negative index
// is invisible in serialized content. D252 rejects it as a protocol error
// rather than indexing a Go slice and crashing the process.
func TestProxyRejectsNegativeContentIndex(t *testing.T) {
	for _, eventType := range []string{"text_start", "text_delta", "text_end", "thinking_start", "thinking_delta", "thinking_end", "toolcall_start", "toolcall_delta", "toolcall_end"} {
		t.Run(eventType, func(t *testing.T) {
			_, err := processProxyEvent(&proxyWireEvent{Type: eventType, ContentIndex: -1}, &ai.AssistantMessage{Content: ai.ContentList{}}, newProxyEventScratch())
			if err == nil || !strings.Contains(err.Error(), "negative content index") {
				t.Fatalf("error=%v; want negative content index", err)
			}
		})
	}
}

// pi v1.3.0 agent/src/proxy.ts assigns contentSignature on every block end,
// but assigns providerThinkingLevel only when the terminal event supplies it.
func TestProxyPreservesOptionalFieldAssignmentSemantics(t *testing.T) {
	for _, thinking := range []bool{false, true} {
		partial := &ai.AssistantMessage{}
		scratch := newProxyEventScratch()
		prefix := "text"
		if thinking {
			prefix = "thinking"
		}
		_, _ = processProxyEvent(&proxyWireEvent{Type: prefix + "_start"}, partial, scratch)
		signature := "sig"
		_, _ = processProxyEvent(&proxyWireEvent{Type: prefix + "_end", ContentSignature: &signature}, partial, scratch)
		_, _ = processProxyEvent(&proxyWireEvent{Type: prefix + "_end"}, partial, scratch)
		switch content := partial.Content[0].(type) {
		case ai.TextContent:
			if content.TextSignature != nil {
				t.Error("absent text signature did not clear previous signature")
			}
		case ai.ThinkingContent:
			if content.ThinkingSignature != nil {
				t.Error("absent thinking signature did not clear previous signature")
			}
		}
	}
	for _, eventType := range []string{"done", "error"} {
		level := "high"
		partial := &ai.AssistantMessage{ProviderThinkingLevel: &level}
		_, _ = processProxyEvent(&proxyWireEvent{Type: eventType, Reason: ai.StopStop}, partial, newProxyEventScratch())
		if partial.ProviderThinkingLevel == nil || *partial.ProviderThinkingLevel != "high" {
			t.Errorf("%s cleared absent provider thinking level", eventType)
		}
	}
}

// TestStreamProxyReconstructsMessage covers the client side: the request shape
// and the event-by-event rebuild of the assistant message.
func TestStreamProxyReconstructsMessage(t *testing.T) {
	var authHeader string
	var body map[string]json.RawMessage
	mux := http.NewServeMux()
	mux.HandleFunc("/api/stream", func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		events := []string{
			`{"type":"start"}`,
			`{"type":"text_start","contentIndex":0}`,
			`{"type":"text_delta","contentIndex":0,"delta":"Hel"}`,
			`{"type":"text_delta","contentIndex":0,"delta":"lo"}`,
			`{"type":"text_end","contentIndex":0,"contentSignature":"sig"}`,
			`{"type":"toolcall_start","contentIndex":1,"id":"c1","toolName":"read"}`,
			`{"type":"toolcall_delta","contentIndex":1,"delta":"{\"pa"}`,
			`{"type":"toolcall_delta","contentIndex":1,"delta":"th\":\"a\"}"}`,
			`{"type":"toolcall_end","contentIndex":1,"toolCall":{"id":"c1","name":"read","arguments":{"path":"a"}}}`,
			`{"type":"done","reason":"stop","usage":{"input":3,"output":4,"cacheRead":0,"cacheWrite":0,"totalTokens":7,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}}}`,
		}
		for _, event := range events {
			_, _ = io.WriteString(w, "data: "+event+"\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	model := &ai.Model{ID: "m", Provider: "p", API: ai.APIOpenAIResponses}
	stream := StreamProxy(model, proxyContext(), ProxyStreamOptions{ProxyURL: server.URL, AuthToken: "tok"})
	result, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if authHeader != "Bearer tok" {
		t.Fatalf("authorization = %q", authHeader)
	}
	var contextBody struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(body["context"], &contextBody); err != nil || len(contextBody.Messages) != 1 {
		t.Fatalf("context = %s, %v", body["context"], err)
	}
	if len(body["model"]) == 0 {
		t.Fatal("model missing from the request")
	}
	if result.StopReason != ai.StopStop {
		t.Fatalf("stopReason = %q", result.StopReason)
	}
	if len(result.Content) != 2 {
		t.Fatalf("content = %+v", result.Content)
	}
	text, ok := result.Content[0].(ai.TextContent)
	if !ok || text.Text != "Hello" || text.TextSignature == nil || *text.TextSignature != "sig" {
		t.Fatalf("text = %+v", result.Content[0])
	}
	call, ok := result.Content[1].(ai.ToolCall)
	if !ok || call.ID != "c1" || call.Name != "read" || string(call.Arguments) != `{"path":"a"}` {
		t.Fatalf("toolcall = %+v", result.Content[1])
	}
	if result.Usage.Output != 4 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

// TestStreamProxyCleanEOFFails covers a server that drops the response without a
// terminal event.
func TestStreamProxyCleanEOFFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"start\"}\n\n")
	}))
	defer server.Close()
	model := &ai.Model{ID: "m", Provider: "p", API: ai.APIOpenAIResponses}
	result, err := StreamProxy(model, proxyContext(), ProxyStreamOptions{ProxyURL: server.URL}).Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != ai.StopError || result.ErrorMessage == nil ||
		!strings.Contains(*result.ErrorMessage, "Connection closed by proxy server") {
		t.Fatalf("result = %+v", result)
	}
}

// TestStreamProxyHTTPError covers the error-response body.
func TestStreamProxyHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"invalid token"}`)
	}))
	defer server.Close()
	model := &ai.Model{ID: "m", Provider: "p", API: ai.APIOpenAIResponses}
	result, err := StreamProxy(model, proxyContext(), ProxyStreamOptions{ProxyURL: server.URL}).Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorMessage == nil || *result.ErrorMessage != "Proxy error: invalid token" {
		t.Fatalf("error = %+v", result.ErrorMessage)
	}
}

// pi v1.3.0 agent/src/proxy.ts rejects every !response.ok status and formats
// the status code once, followed by response.statusText when JSON has no error.
func TestStreamProxyHTTPStatusFallback(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusMultipleChoices} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer server.Close()
			model := &ai.Model{ID: "m", Provider: "p", API: ai.APIOpenAIResponses}
			result, err := StreamProxy(model, proxyContext(), ProxyStreamOptions{ProxyURL: server.URL}).Result(context.Background())
			want := fmt.Sprintf("Proxy error: %d %s", status, http.StatusText(status))
			if err != nil || result.ErrorMessage == nil || *result.ErrorMessage != want {
				t.Fatalf("result=%+v error=%v; want %s", result, err, want)
			}
		})
	}
}

// proxy.ts parses each SSE line with JSON.parse outside a try, so a malformed
// event fails the stream instead of being skipped.
func TestStreamProxyMalformedEventFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"start\"}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\n\n")
		_, _ = io.WriteString(w, `data: {"type":"done","reason":"stop","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}}}`+"\n\n")
	}))
	defer server.Close()
	model := &ai.Model{ID: "m", Provider: "p", API: ai.APIOpenAIResponses}
	result, err := StreamProxy(model, proxyContext(), ProxyStreamOptions{ProxyURL: server.URL}).Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != ai.StopError || result.ErrorMessage == nil {
		t.Fatalf("result = %+v; want a parse error instead of the later done event", result)
	}
}

// proxy.ts throws when a delta does not match the content block it names.
func TestStreamProxyRejectsDeltaForOtherContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"start\"}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"text_delta\",\"contentIndex\":0,\"delta\":\"x\"}\n\n")
		_, _ = io.WriteString(w, `data: {"type":"done","reason":"stop","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}}}`+"\n\n")
	}))
	defer server.Close()
	model := &ai.Model{ID: "m", Provider: "p", API: ai.APIOpenAIResponses}
	result, err := StreamProxy(model, proxyContext(), ProxyStreamOptions{ProxyURL: server.URL}).Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != ai.StopError || result.ErrorMessage == nil ||
		*result.ErrorMessage != "Received text_delta for non-text content" {
		t.Fatalf("result = %+v; want the delta mismatch error", result)
	}
}
