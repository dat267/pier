package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Streaming tests replicating the Anthropic SSE wire format (the same shapes
// upstream's mock-server tests replay).

// sseServer serves a scripted SSE response for POST /v1/messages.
func sseServer(t *testing.T, events []string, capture func(r *http.Request, body []byte)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			body := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(body)
			capture(r, body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		for _, event := range events {
			_, _ = w.Write([]byte(event))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}))
}

// anthropic-messages.ts hasHeader uses String.trim before accepting auth headers.
func TestStreamAnthropicErrorPaths(t *testing.T) {
	t.Run("sse error event", func(t *testing.T) {
		model := testAnthropicModel()
		server := sseServer(t, []string{
			"event: error\ndata: {\"type\":\"error\",\"error\":{\"message\":\"overloaded\"}}\n\n",
		}, nil)
		model.BaseURL = server.URL
		defer server.Close()
		stream := StreamAnthropic(model, NormalizeContext(Context{
			Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
		}), &AnthropicOptions{APIKey: "k"})
		msg, _ := stream.Result(context.Background())
		if msg.StopReason != StopError || msg.ErrorMessage == nil || !strings.Contains(*msg.ErrorMessage, "overloaded") {
			t.Fatalf("message = %+v", msg)
		}
	})

	t.Run("ended before message_stop", func(t *testing.T) {
		model := testAnthropicModel()
		server := sseServer(t, []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"model\":\"claude-opus-4-5\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n",
		}, nil)
		model.BaseURL = server.URL
		defer server.Close()
		stream := StreamAnthropic(model, NormalizeContext(Context{
			Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
		}), &AnthropicOptions{APIKey: "k"})
		msg, _ := stream.Result(context.Background())
		if msg.StopReason != StopError || !strings.Contains(*msg.ErrorMessage, "ended before message_stop") {
			t.Fatalf("message = %+v", msg)
		}
	})

	t.Run("missing api key fails before request", func(t *testing.T) {
		model := testAnthropicModel()
		called := false
		server := sseServer(t, nil, func(*http.Request, []byte) { called = true })
		model.BaseURL = server.URL
		defer server.Close()
		bomHeader := "\uFEFF"
		stream := StreamAnthropic(model, NormalizeContext(Context{
			Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
		}), &AnthropicOptions{Headers: map[string]*string{"Authorization": &bomHeader}})
		msg, _ := stream.Result(context.Background())
		if called {
			t.Fatal("no request should be made without auth")
		}
		if msg.StopReason != StopError || *msg.ErrorMessage != "No API key for provider: anthropic" {
			t.Fatalf("message = %+v", msg)
		}
	})

	t.Run("unhandled stop reason", func(t *testing.T) {
		model := testAnthropicModel()
		server := sseServer(t, []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"model\":\"claude-opus-4-5\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"mystery\"}}\n\n",
		}, nil)
		model.BaseURL = server.URL
		defer server.Close()
		stream := StreamAnthropic(model, NormalizeContext(Context{
			Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
		}), &AnthropicOptions{APIKey: "k"})
		msg, _ := stream.Result(context.Background())
		if msg.StopReason != StopError || !strings.Contains(*msg.ErrorMessage, "Unhandled stop reason: mystery") {
			t.Fatalf("message = %+v", msg)
		}
	})

	t.Run("refusal with explanation", func(t *testing.T) {
		model := testAnthropicModel()
		server := sseServer(t, []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"model\":\"claude-opus-4-5\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"refusal\",\"stop_details\":{\"explanation\":\"nope\"}}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		}, nil)
		model.BaseURL = server.URL
		defer server.Close()
		stream := StreamAnthropic(model, NormalizeContext(Context{
			Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
		}), &AnthropicOptions{APIKey: "k"})
		msg, _ := stream.Result(context.Background())
		if msg.StopReason != StopError || *msg.ErrorMessage != "nope" {
			t.Fatalf("message = %+v", msg)
		}
	})
}

// #10409, packages/ai/src/api/anthropic-messages.ts retries only documented strict-tool limits.
func TestStreamAnthropicRetriesPreferredToolsWithoutStrictAfterGrammarRejection(t *testing.T) {
	model := testAnthropicModel()
	tool := Tool{
		Name: "lookup", Description: "Look up a value",
		Parameters:          json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"},"optional":{"type":"number"}},"required":["value"]}`),
		ConstrainedSampling: ConstrainedSamplingValue{Set: true, Config: &ConstrainedSamplingConfig{Type: "json_schema", Strict: "prefer"}},
	}
	requiredTool := tool
	requiredTool.Name = "required"
	requiredTool.ConstrainedSampling = ConstrainedSamplingValue{Set: true, Config: &ConstrainedSamplingConfig{Type: "json_schema", Strict: "require"}}
	transcript := NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hello"}, Timestamp: 1}},
		Tools:    []Tool{tool, requiredTool},
	})
	type requestTool struct {
		Strict      *bool           `json:"strict"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	var attempts int
	var payloads [][]requestTool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		var payload struct {
			Tools []requestTool `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		payloads = append(payloads, payload.Tools)
		if attempts == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"The compiled grammar is too large"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"model\":\"claude-opus-4-5\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	model.BaseURL = server.URL
	defer server.Close()

	message, err := StreamAnthropic(model, transcript, &AnthropicOptions{APIKey: "test-key"}).Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if message.StopReason != StopStop || attempts != 2 {
		t.Fatalf("message stop=%s attempts=%d error=%v", message.StopReason, attempts, message.ErrorMessage)
	}
	if len(payloads) != 2 || len(payloads[0]) != 2 || len(payloads[1]) != 2 {
		t.Fatalf("payload tools = %+v", payloads)
	}
	if payloads[0][0].Strict == nil || !*payloads[0][0].Strict || payloads[1][0].Strict != nil {
		t.Fatalf("preferred strict flags = %v then %v", payloads[0][0].Strict, payloads[1][0].Strict)
	}
	if payloads[1][1].Strict == nil || !*payloads[1][1].Strict {
		t.Fatalf("required strict tool was downgraded: %+v", payloads[1][1])
	}
	var fallbackSchema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(payloads[1][0].InputSchema, &fallbackSchema); err != nil || len(fallbackSchema.Required) != 1 || fallbackSchema.Required[0] != "value" {
		t.Fatalf("fallback schema = %s, err = %v", payloads[1][0].InputSchema, err)
	}
	if len(message.Diagnostics) != 1 || message.Diagnostics[0].Type != "anthropic_strict_tools_fallback" {
		t.Fatalf("diagnostics = %+v", message.Diagnostics)
	}
}

func TestStreamAnthropicHTTPRetry(t *testing.T) {
	model := testAnthropicModel()
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.Header().Set("retry-after-ms", "10")
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"type":"error","error":{"message":"rate limited"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"model\":\"claude-opus-4-5\",\"usage\":{\"input_tokens\":2,\"output_tokens\":0}}}\n\n" +
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	model.BaseURL = server.URL
	defer server.Close()

	maxRetries := 5
	stream := StreamAnthropic(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &AnthropicOptions{APIKey: "k", MaxRetries: &maxRetries})
	msg, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if msg.StopReason != StopStop {
		t.Fatalf("stopReason = %s", msg.StopReason)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d; want 3", attempts)
	}
}

func TestStreamAnthropicRetryDelayCapFails(t *testing.T) {
	model := testAnthropicModel()
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("retry-after", "120")
		w.WriteHeader(429)
	}))
	model.BaseURL = server.URL
	defer server.Close()

	maxRetries := 3
	stream := StreamAnthropic(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &AnthropicOptions{APIKey: "k", MaxRetries: &maxRetries})
	msg, _ := stream.Result(context.Background())
	if msg.StopReason != StopError || !strings.Contains(*msg.ErrorMessage, "Server requested 120s retry delay") {
		t.Fatalf("message = %+v", msg)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d; want 1 (cap fails immediately)", attempts)
	}
}

// Upstream builds Copilot's per-request headers inside stream()
// (packages/ai/src/api/anthropic-messages.ts, buildCopilotDynamicHeaders):
// X-Initiator marks the request user- or agent-initiated, Openai-Intent is
// always sent, and Copilot-Vision-Request is added when the transcript carries
// images. The port had the helper and the DynamicHeaders merge, but nothing
// filled it in, so a Copilot messages request went out without them.
func TestStreamAnthropicCopilotDynamicHeaders(t *testing.T) {
	events := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-opus-4-5\",\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}
	tests := []struct {
		name          string
		messages      []Message
		wantInitiator string
		wantVision    string
	}{
		{
			name: "agent initiated",
			messages: []Message{
				&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
				&AssistantMessage{Content: []Content{TextContent{Text: "hello"}}, Timestamp: 2},
			},
			wantInitiator: "agent",
		},
		{
			name: "user initiated with image",
			messages: []Message{
				&UserMessage{Content: StringOrBlocks{Blocks: []Content{
					TextContent{Text: "look"},
					ImageContent{MimeType: "image/png", Data: "AAAA"},
				}}, Timestamp: 1},
			},
			wantInitiator: "user",
			wantVision:    "true",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := map[string]string{}
			server := sseServer(t, events, func(r *http.Request, _ []byte) {
				for _, name := range []string{"X-Initiator", "Openai-Intent", "Copilot-Vision-Request"} {
					got[name] = r.Header.Get(name)
				}
			})
			defer server.Close()

			model := testAnthropicModel()
			model.Provider = "github-copilot"
			model.BaseURL = server.URL
			stream := StreamAnthropic(model, NormalizeContext(Context{Messages: tt.messages}),
				&AnthropicOptions{APIKey: "copilot-token"})
			for {
				if _, ok := stream.Next(context.Background()); !ok {
					break
				}
			}
			if _, err := stream.Result(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got["X-Initiator"] != tt.wantInitiator {
				t.Fatalf("X-Initiator = %q, want %q", got["X-Initiator"], tt.wantInitiator)
			}
			if got["Openai-Intent"] != "conversation-edits" {
				t.Fatalf("Openai-Intent = %q, want conversation-edits", got["Openai-Intent"])
			}
			if got["Copilot-Vision-Request"] != tt.wantVision {
				t.Fatalf("Copilot-Vision-Request = %q, want %q", got["Copilot-Vision-Request"], tt.wantVision)
			}
		})
	}
}

// pi v1.4.0 api/anthropic-messages.ts message_start assigns
// `usage.cacheWrite1h = cache_creation?.ephemeral_1h_input_tokens || 0`, so the
// key is always present in a persisted Anthropic assistant message.
func TestAnthropicStreamUsageAlwaysReportsCacheWrite1h(t *testing.T) {
	events := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-sonnet-4-5\",\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}
	server := sseServer(t, events, nil)
	defer server.Close()

	model := testAnthropicModel()
	model.BaseURL = server.URL
	stream := StreamAnthropic(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &AnthropicOptions{APIKey: "test-key"})
	msg, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := MarshalJSON(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Usage map[string]json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	value, ok := decoded.Usage["cacheWrite1h"]
	if !ok {
		t.Fatalf("usage.cacheWrite1h missing: %s", encoded)
	}
	if string(value) != "0" {
		t.Fatalf("usage.cacheWrite1h = %s, want 0", value)
	}
}
