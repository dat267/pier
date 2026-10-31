package ai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// pi v1.4.0 (4a080252), api/mistral-conversations.ts. Regenerate:
//
//	node --experimental-strip-types ai/testdata/mistral-stream/generate.mjs ./pi > ai/testdata/mistral-stream/golden.json
//
// Compare event scalars and terminal messages, not the live partial helper or clocks.
func TestMistralStreamEventsMatchUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/mistral-stream/golden.json")
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
		t.Fatal("empty Mistral stream corpus")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(fixture.SSE))
			}))
			defer server.Close()
			model := mistralModel(server.URL)
			model.Cost = ModelCost{Input: 2, Output: 6, CacheRead: 0.5}
			stream := StreamMistralConversations(model, mistralUserContext(), &MistralOptions{APIKey: "test-key"})
			checkProjectedEvents(t, stream, fixture.Events)
		})
	}
}
