package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

func proxyContext() ai.TranscriptContext {
	return ai.TranscriptContext{Messages: []ai.Message{
		&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}, Timestamp: 1},
	}}
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
