package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), api/{anthropic-messages,
// openai-completions,openai-codex-responses}.ts stream payload replacement.
// Regenerate: node --experimental-strip-types testdata/provider-hooks/generate.mjs ../pi > testdata/provider-hooks/golden.json
func TestProviderPayloadReplacementsMatchUpstream(t *testing.T) {
	data, err := os.ReadFile("testdata/provider-hooks/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Model       Model             `json:"model"`
		Messages    []json.RawMessage `json:"messages"`
		Replacement json.RawMessage   `json:"replacement"`
		Body        string            `json:"body"`
		Path        string            `json:"path"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(string(fixture.Model.API), func(t *testing.T) {
			bodies := make(chan string, 1)
			paths := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				bodies <- string(b)
				paths <- r.URL.Path
				w.Header().Set("Content-Type", "text/event-stream")
				switch fixture.Model.API {
				case APIOpenAICompletions:
					io.WriteString(w, "data: {\"id\":\"r\",\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				case APIAnthropicMessages:
					io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"r\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
				default:
					io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":0,\"output_tokens\":0,\"total_tokens\":0}}}\n\n")
				}
			}))
			defer server.Close()
			model := fixture.Model
			model.BaseURL = strings.ReplaceAll(model.BaseURL, "https://invalid.test", server.URL)
			var messages []Message
			for _, raw := range fixture.Messages {
				m, err := UnmarshalMessage(raw)
				if err != nil {
					t.Fatal(err)
				}
				messages = append(messages, m)
			}
			options := StreamOptions{APIKey: "token", Env: ProviderEnv{}, OnPayload: func(json.RawMessage, *Model) json.RawMessage { return fixture.Replacement }}
			var stream *AssistantMessageEventStream
			switch model.API {
			case APIOpenAICompletions:
				stream = StreamOpenAICompletions(&model, TranscriptContext{Messages: messages}, &OpenAICompletionsOptions{StreamOptions: options})
			case APIAnthropicMessages:
				stream = StreamAnthropic(&model, TranscriptContext{Messages: messages}, &AnthropicOptions{StreamOptions: options})
			case APIOpenAICodexResponses:
				options.APIKey = codexToken(t, "acct")
				stream = StreamOpenAICodexResponses(&model, TranscriptContext{Messages: messages}, &OpenAICodexResponsesOptions{StreamOptions: options, Transport: "sse"})
			default:
				t.Fatalf("unknown API %q", model.API)
			}
			result, err := stream.Result(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if result.StopReason != StopStop {
				t.Fatalf("result = %+v", result)
			}
			if got := <-paths; got != fixture.Path {
				t.Errorf("path = %s, want %s", got, fixture.Path)
			}
			if got := <-bodies; got != fixture.Body {
				t.Errorf("body = %s\nwant   %s", got, fixture.Body)
			}
		})
	}
}
