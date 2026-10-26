package ai

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

// pi v1.4.0 (4a080252), api/mistral-conversations.ts streamSimple selects
// reasoning_effort from the model map, not from hardcoded model ids.
// Regenerate:
//
//	node --experimental-strip-types ai/testdata/mistral-simple/generate.mjs ./pi > ai/testdata/mistral-simple/golden.json
func TestMistralSimpleReasoningMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/mistral-simple/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	type reasoningPayload struct {
		ReasoningEffort string `json:"reasoningEffort,omitempty"`
		PromptMode      string `json:"promptMode,omitempty"`
	}
	var fixtures []struct {
		Name      string           `json:"name"`
		Model     Model            `json:"model"`
		Reasoning ThinkingLevel    `json:"reasoning"`
		Expected  reasoningPayload `json:"expected"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty Mistral simple corpus")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			server := sseServer(t, []string{"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"}, nil)
			defer server.Close()
			fixture.Model.BaseURL = server.URL
			var got reasoningPayload
			stream := StreamMistralConversationsSimple(&fixture.Model, mistralUserContext(), &SimpleStreamOptions{
				Reasoning: fixture.Reasoning,
				StreamOptions: StreamOptions{APIKey: "test-key", OnPayload: func(payload json.RawMessage, model *Model) json.RawMessage {
					if err := json.Unmarshal(payload, &got); err != nil {
						t.Error(err)
					}
					return nil
				}},
			})
			message, err := stream.Result(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if message.StopReason != StopStop {
				t.Fatalf("stream failed: %+v", message)
			}
			if got != fixture.Expected {
				t.Fatalf("reasoning payload = %+v, want %+v", got, fixture.Expected)
			}
		})
	}
}
