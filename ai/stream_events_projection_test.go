package ai

import (
	"context"
	"encoding/json"
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
