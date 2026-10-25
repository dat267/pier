package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"
)

// pi v1.4.0 api/anthropic-messages.ts stream transitions, executed by the
// pinned module. Regenerate:
//
//	node --experimental-strip-types ai/testdata/anthropic-stream/generate.mjs ./pi \
//	  > ai/testdata/anthropic-stream/golden.json
//
// Only the event's own scalars and the terminal message are compared:
// `partial` is the documented live response-so-far helper rather than an
// event-time snapshot, and timestamps/durations are wall-clock values.
func TestAnthropicStreamEventsMatchUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/anthropic-stream/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name   string            `json:"name"`
		SSE    string            `json:"sse"`
		Events []json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty anthropic stream corpus")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(fixture.SSE))
			}))
			defer server.Close()

			model := testAnthropicModel()
			model.ID = "claude-sonnet-4-5"
			model.Name = "Claude Sonnet 4.5"
			model.BaseURL = server.URL
			model.Cost = ModelCost{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75}

			stream := StreamAnthropic(model, NormalizeContext(Context{
				Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
			}), &AnthropicOptions{StreamOptions: StreamOptions{APIKey: "test-key"}})

			var got []string
			for {
				event, ok := stream.Next(context.Background())
				if !ok {
					break
				}
				encoded, err := json.Marshal(projectEvent(t, event))
				if err != nil {
					t.Fatalf("marshal projection: %v", err)
				}
				got = append(got, string(encoded))
			}
			if len(got) != len(fixture.Events) {
				t.Fatalf("event count = %d, want %d\ngot: %v", len(got), len(fixture.Events), got)
			}
			for i := range fixture.Events {
				wantBytes, err := json.Marshal(canonicalJSON(t, fixture.Events[i]))
				if err != nil {
					t.Fatal(err)
				}
				want := string(wantBytes)
				if got[i] != want {
					t.Errorf("event %d differs\n got: %s\nwant: %s", i, got[i], want)
				}
			}
		})
	}
}

// projectEvent mirrors the generator's projection.
func projectEvent(t *testing.T, event AssistantMessageEvent) any {
	t.Helper()
	out := map[string]any{"type": event.Type}
	switch event.Type {
	case EventTextStart, EventTextDelta, EventTextEnd,
		EventThinkingStart, EventThinkingDelta, EventThinkingEnd,
		EventToolcallStart, EventToolcallDelta, EventToolcallEnd:
		out["contentIndex"] = event.ContentIndex
	}
	switch event.Type {
	case EventTextDelta, EventThinkingDelta, EventToolcallDelta:
		out["delta"] = event.Delta
	}
	switch event.Type {
	case EventTextEnd, EventThinkingEnd:
		out["content"] = event.Content
	}
	if event.ToolCall != nil && event.Type == EventToolcallEnd {
		toolCall := map[string]any{
			"id":        normalizeSyntheticToolCallID(event.ToolCall.ID),
			"name":      event.ToolCall.Name,
			"arguments": canonicalRaw(t, event.ToolCall.Arguments),
		}
		if event.ToolCall.ThoughtSignature != nil {
			toolCall["thoughtSignature"] = *event.ToolCall.ThoughtSignature
		}
		out["toolCall"] = toolCall
	}
	switch event.Type {
	case EventDone, EventError:
		out["reason"] = event.Reason
	}
	if event.Message != nil && event.Type == EventDone {
		out["message"] = projectMessage(t, event.Message)
	}
	if event.Error != nil && event.Type == EventError {
		out["error"] = projectMessage(t, event.Error)
	}
	return out
}

func projectMessage(t *testing.T, message *AssistantMessage) any {
	t.Helper()
	content := make([]any, 0, len(message.Content))
	for _, block := range message.Content {
		switch typed := block.(type) {
		case TextContent:
			projected := map[string]any{"type": "text", "text": typed.Text}
			if typed.TextSignature != nil {
				projected["textSignature"] = *typed.TextSignature
			}
			content = append(content, projected)
		case ThinkingContent:
			signature := ""
			if typed.ThinkingSignature != nil {
				signature = *typed.ThinkingSignature
			}
			projected := map[string]any{
				"type":              "thinking",
				"thinking":          typed.Thinking,
				"thinkingSignature": signature,
			}
			if typed.Redacted {
				projected["redacted"] = true
			}
			content = append(content, projected)
		case ToolCall:
			projected := map[string]any{
				"type":      "toolCall",
				"id":        normalizeSyntheticToolCallID(typed.ID),
				"name":      typed.Name,
				"arguments": canonicalRaw(t, typed.Arguments),
			}
			if typed.ThoughtSignature != nil {
				projected["thoughtSignature"] = *typed.ThoughtSignature
			}
			content = append(content, projected)
		default:
			t.Fatalf("unexpected content block %T", block)
		}
	}
	out := map[string]any{
		"content":    content,
		"api":        message.API,
		"provider":   message.Provider,
		"model":      message.Model,
		"stopReason": message.StopReason,
		"usage":      canonicalRaw(t, mustMarshalTestJSON(t, message.Usage)),
	}
	if message.ResponseModel != nil {
		out["responseModel"] = *message.ResponseModel
	}
	if message.ResponseID != nil {
		out["responseId"] = *message.ResponseID
	}
	if message.RawStopReason != nil {
		out["rawStopReason"] = *message.RawStopReason
	}
	if message.ErrorMessage != nil {
		out["errorMessage"] = *message.ErrorMessage
	}
	if message.ThinkingLevel != nil {
		out["thinkingLevel"] = *message.ThinkingLevel
	}
	if message.ProviderThinkingLevel != nil {
		out["providerThinkingLevel"] = *message.ProviderThinkingLevel
	}
	return out
}

// normalizeSyntheticToolCallID hides the process-dependent parts of the id
// Google providers synthesize (`<name>_<Date.now()>_<counter>`, where the
// counter is module-global in both implementations).
var syntheticToolCallID = regexp.MustCompile(`^(.+)_\d{10,}_\d+$`)

func normalizeSyntheticToolCallID(id string) string {
	if match := syntheticToolCallID.FindStringSubmatch(id); match != nil {
		return match[1] + "_<ts>_<n>"
	}
	return id
}

func mustMarshalTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := MarshalJSON(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return encoded
}

// canonicalRaw returns the value decoded so encoding/json sorts object keys.
func canonicalRaw(t *testing.T, raw []byte) any {
	t.Helper()
	return canonicalJSON(t, raw)
}

func canonicalJSON(t *testing.T, raw []byte) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return sortValue(value)
}

func sortValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			typed[key] = sortValue(item)
		}
		return typed
	case []any:
		for i := range typed {
			typed[i] = sortValue(typed[i])
		}
		return typed
	default:
		return value
	}
}
