package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// pi v1.4.0 api/google-generative-ai.ts and google-shared.ts stream
// transitions, executed by the pinned module. Regenerate:
//
//	node --experimental-strip-types ai/testdata/google-stream/generate.mjs ./pi \
//	  > ai/testdata/google-stream/golden.json
//
// Only the event's own scalars and the terminal message are compared; see
// anthropic_stream_events_parity_test.go for why `partial` is excluded.
func TestGoogleStreamEventsMatchUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/google-stream/golden.json")
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
		t.Fatal("empty google stream corpus")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(fixture.SSE))
			}))
			defer server.Close()

			model := testGoogleModel()
			model.ID = "gemini-3-pro"
			model.Name = "Gemini 3 Pro"
			model.BaseURL = server.URL + "/v1beta"
			model.Cost = ModelCost{ModelCostRates: ModelCostRates{Input: 2, Output: 12, CacheRead: 0.5}}

			stream := StreamGoogleGenerativeAI(model, NormalizeContext(Context{
				Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
			}), &GoogleOptions{StreamOptions: StreamOptions{APIKey: "test-key"}})

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
