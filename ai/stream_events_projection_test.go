package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// checkProjectedEvents drains stream and compares every projected event with
// the generator fixtures byte for byte. One home for the comparison the
// per-provider stream-events parity tests share.
func checkProjectedEvents(t *testing.T, stream *AssistantMessageEventStream, want []json.RawMessage) {
	t.Helper()
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
	if len(got) != len(want) {
		t.Fatalf("event count = %d, want %d\ngot: %v", len(got), len(want), got)
	}
	for index := range want {
		wantBytes, err := json.Marshal(canonicalJSON(t, want[index]))
		if err != nil {
			t.Fatal(err)
		}
		if got[index] != string(wantBytes) {
			t.Errorf("event %d differs\n got: %s\nwant: %s", index, got[index], wantBytes)
		}
	}
}

// runStreamEventsCorpus replays each case of a Node-generated stream golden
// (name, SSE body, projected events) through a local SSE server and checks
// the events start produces against the reference's.
func runStreamEventsCorpus(t *testing.T, path, label string, start func(baseURL string) *AssistantMessageEventStream) {
	t.Helper()
	raw, err := os.ReadFile(path)
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
		t.Fatalf("empty %s corpus", label)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(fixture.SSE))
			}))
			defer server.Close()
			checkProjectedEvents(t, start(server.URL), fixture.Events)
		})
	}
}
