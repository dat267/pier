package ai

import (
	"context"
	"math"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365), api/anthropic-messages.ts
// sums usage as Numbers on message_start and message_delta. D367 saturates
// totals outside the Go int64 representation instead of wrapping negative.
func TestAnthropicStreamDoesNotWrapUsageTotals(t *testing.T) {
	for _, test := range []struct {
		name, initialUsage, deltaUsage string
		want                           int64
	}{
		{
			name:         "start usage",
			initialUsage: `{"input_tokens":6000000000000000000,"output_tokens":6000000000000000000}`,
			want:         math.MaxInt64,
		},
		{
			name:         "delta usage",
			initialUsage: `{"input_tokens":10,"output_tokens":1}`,
			deltaUsage:   `,"usage":{"input_tokens":6000000000000000000,"output_tokens":6000000000000000000}`,
			want:         math.MaxInt64,
		},
		{
			name:         "ordinary usage",
			initialUsage: `{"input_tokens":10,"output_tokens":1}`,
			deltaUsage:   `,"usage":{"output_tokens":4}`,
			want:         14,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := sseServer(t, []string{
				"event: message_start\ndata: " + `{"type":"message_start","message":{"id":"msg_1","usage":` + test.initialUsage + "}}\n\n",
				"event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn"}` + test.deltaUsage + "}\n\n",
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
			}, nil)
			defer server.Close()
			model := testAnthropicModel()
			model.BaseURL = server.URL
			message, err := StreamAnthropic(model, NormalizeContext(Context{}), &AnthropicOptions{APIKey: "test-key"}).Result(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if message.StopReason != StopStop {
				t.Fatalf("stop reason = %s, error = %v", message.StopReason, message.ErrorMessage)
			}
			if message.Usage.TotalTokens != test.want {
				t.Fatalf("total tokens = %d, want %d", message.Usage.TotalTokens, test.want)
			}
		})
	}
}

// Reference: pi v1.5.0 (f9a26365), api/mistral-conversations.ts
// consumeChatStream adds token fields when total_tokens is absent (D367).
func TestMistralStreamDoesNotWrapFallbackUsageTotal(t *testing.T) {
	server := sseServer(t, []string{
		"data: " + `{"choices":[{"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":6000000000000000000,"completion_tokens":6000000000000000000}}` + "\n\ndata: [DONE]\n\n",
	}, nil)
	defer server.Close()
	message, err := StreamMistralConversations(mistralModel(server.URL), mistralUserContext(), &MistralOptions{APIKey: "test-key"}).Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if message.StopReason != StopStop {
		t.Fatalf("stop reason = %s, error = %v", message.StopReason, message.ErrorMessage)
	}
	if message.Usage.TotalTokens != math.MaxInt64 {
		t.Fatalf("fallback total tokens = %d, want %d", message.Usage.TotalTokens, int64(math.MaxInt64))
	}
}

// Reference: pi v1.5.0 (f9a26365), api/google-generative-ai.ts and
// google-vertex.ts add candidate and thought counts as Numbers (D367).
func TestGoogleStreamDoesNotWrapOutputIncludingThoughts(t *testing.T) {
	server := sseServer(t, []string{
		"data: " + `{"candidates":[{"content":{"parts":[{"text":"done"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":6000000000000000000,"thoughtsTokenCount":6000000000000000000,"totalTokenCount":9223372036854775807}}` + "\n\n",
	}, nil)
	defer server.Close()
	model := testGoogleModel()
	model.BaseURL = server.URL
	message, err := StreamGoogleGenerativeAI(model, NormalizeContext(Context{}), &GoogleOptions{APIKey: "test-key"}).Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if message.StopReason != StopStop {
		t.Fatalf("stop reason = %s, error = %v", message.StopReason, message.ErrorMessage)
	}
	if message.Usage.Output != math.MaxInt64 {
		t.Fatalf("output tokens including thoughts = %d, want %d", message.Usage.Output, int64(math.MaxInt64))
	}
}

// Reference: pi v1.5.0 (f9a26365), api/openai-responses-shared.ts
// processResponsesStream subtracts both cache counts before clamping input.
func TestResponsesStreamDoesNotWrapOversizedCacheSubtraction(t *testing.T) {
	server := sseServer(t, []string{
		"data: " + `{"type":"response.completed","response":{"status":"completed","output":[],"usage":{"input_tokens":100,"output_tokens":0,"input_tokens_details":{"cached_tokens":9223372036854775807,"cache_write_tokens":9223372036854775807},"total_tokens":9223372036854775807}}}` + "\n\n",
	}, nil)
	defer server.Close()
	model := testResponsesModel()
	model.BaseURL = server.URL
	message, err := StreamOpenAIResponses(model, NormalizeContext(Context{}), &OpenAIResponsesOptions{StreamOptions: StreamOptions{APIKey: "test-key"}}).Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if message.StopReason != StopStop {
		t.Fatalf("stop reason = %s, error = %v", message.StopReason, message.ErrorMessage)
	}
	if message.Usage.Input != 0 {
		t.Fatalf("uncached input tokens = %d, want 0 when caches exceed prompt input", message.Usage.Input)
	}
}

// Reference: pi v1.5.0 (f9a26365), api/bedrock-converse-stream.ts
// handleMetadata adds fallback totals and one-hour cache details as Numbers.
func TestBedrockStreamDoesNotWrapUsageSums(t *testing.T) {
	const tokens int64 = 6000000000000000000
	server := bedrockTestServer(t, [][]byte{
		bedrockEventFrame("messageStart", map[string]any{"role": "assistant"}),
		bedrockEventFrame("messageStop", map[string]any{"stopReason": "end_turn"}),
		bedrockEventFrame("metadata", map[string]any{"usage": map[string]any{
			"inputTokens": tokens, "outputTokens": tokens,
			"cacheDetails": []any{
				map[string]any{"ttl": "1h", "inputTokens": tokens},
				map[string]any{"ttl": "1h", "inputTokens": tokens},
			},
		}}),
	}, nil, nil)
	message, err := StreamBedrockConverse(bedrockTestModel(server.URL), TranscriptContext{}, &BedrockOptions{
		APIKey: "test-key", BearerToken: "test-key", Env: ProviderEnv{"AWS_EC2_METADATA_DISABLED": "true"},
	}).Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if message.StopReason != StopStop {
		t.Fatalf("stop reason = %s, error = %v", message.StopReason, message.ErrorMessage)
	}
	if message.Usage.TotalTokens != math.MaxInt64 {
		t.Errorf("fallback total tokens = %d, want %d", message.Usage.TotalTokens, int64(math.MaxInt64))
	}
	if message.Usage.CacheWrite1h == nil {
		t.Fatal("one-hour cache usage is absent")
	}
	if *message.Usage.CacheWrite1h != math.MaxInt64 {
		t.Errorf("one-hour cache usage = %d, want %d", *message.Usage.CacheWrite1h, int64(math.MaxInt64))
	}
}
