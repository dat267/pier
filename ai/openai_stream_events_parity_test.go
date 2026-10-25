package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// pi v1.4.0 api/openai-completions.ts stream transitions, executed by the
// pinned module. Regenerate:
//
//	node --experimental-strip-types ai/testdata/openai-stream/generate.mjs ./pi \
//	  > ai/testdata/openai-stream/golden.json
//
// Only the event's own scalars and the terminal message are compared; see
// anthropic_stream_events_parity_test.go for why `partial` is excluded.
func TestOpenAIStreamEventsMatchUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/openai-stream/golden.json")
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
		t.Fatal("empty openai stream corpus")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(fixture.SSE))
			}))
			defer server.Close()

			model := testOpenAIModel()
			model.ID = "gpt-5"
			model.Name = "GPT-5"
			model.BaseURL = server.URL + "/v1"
			model.Cost = ModelCost{ModelCostRates: ModelCostRates{Input: 1.25, Output: 10, CacheRead: 0.125}}

			stream := StreamOpenAICompletions(model, NormalizeContext(Context{
				Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
			}), &OpenAICompletionsOptions{StreamOptions: StreamOptions{APIKey: "test-key"}})

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
				if got[i] != string(wantBytes) {
					t.Errorf("event %d differs\n got: %s\nwant: %s", i, got[i], wantBytes)
				}
			}
		})
	}
}
