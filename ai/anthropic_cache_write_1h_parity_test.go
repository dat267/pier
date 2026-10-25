package ai

import (
	"context"
	"encoding/json"
	"testing"
)

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
	}), &AnthropicOptions{StreamOptions: StreamOptions{APIKey: "test-key"}})
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
