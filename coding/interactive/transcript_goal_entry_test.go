package interactive

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
)

// A goal lifecycle entry renders its card both live (a freshly appended entry,
// whose Raw line is not populated) and when replayed from disk. The renderer
// receives the entry's payload, matching the extension entry-renderer contract.
func TestTranscriptRendersGoalEntryCards(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"operation": "create",
		"goal": map[string]any{
			"version": 1, "id": "g1", "revision": 1, "objective": "ship the widget",
			"phase": "active", "createdAt": 1, "updatedAt": 1,
		},
		"timestamp": 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, testCase := range []struct {
		name  string
		entry *coding.SessionEntry
	}{
		{
			name: "live",
			entry: &coding.SessionEntry{
				Type: "custom", ID: "e1", CustomType: "pi-goal", Data: payload, Timestamp: "1970-01-01T00:00:00.001Z",
			},
		},
		{
			name:  "replayed",
			entry: loadedGoalEntry(t, payload),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			renderer, _ := newTranscriptTestRenderer(t, false)
			renderer.EntryRenderer = GoalEntryRenderer
			renderer.AddCustomEntryToChat(testCase.entry)
			rendered := strings.Join(renderChat(t, renderer.Chat), "\n")
			if !strings.Contains(rendered, "Goal created") || !strings.Contains(rendered, "ship the widget") {
				t.Fatalf("goal entry card missing: %q", rendered)
			}
		})
	}
}

// loadedGoalEntry parses a persisted custom entry the way the session loader
// does: the payload lands in Data and the whole line in Raw.
func loadedGoalEntry(t *testing.T, payload json.RawMessage) *coding.SessionEntry {
	t.Helper()
	line := `{"type":"custom","customType":"pi-goal","data":` + string(payload) +
		`,"id":"e1","timestamp":"1970-01-01T00:00:00.001Z"}`
	fileEntry, err := coding.UnmarshalFileEntry(line)
	if err != nil {
		t.Fatal(err)
	}
	return fileEntry.Entry
}
