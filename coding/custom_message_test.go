package coding

import (
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

// Upstream appendCustomMessageEntry stores content/details as their actual
// values; the port marshalled the raw JSON text as a string, and the projection
// (CreateCustomMessage via string(entry.Content)) added another escaping layer.
func TestCustomMessageContentRoundTripsRaw(t *testing.T) {
	persist := false
	m := NewSessionManager(t.TempDir(), &SessionManagerOptions{SessionDir: t.TempDir(), Persist: &persist})
	for _, content := range []string{`"hello"`, `[{"type":"text","text":"hi"}]`} {
		id := m.AppendCustomMessageEntry("t", json.RawMessage(content), true, json.RawMessage(`{"k":1}`))
		var entry *SessionEntry
		for i := range m.GetEntries() {
			if m.GetEntries()[i].ID == id {
				entry = &m.GetEntries()[i]
			}
		}
		if entry == nil {
			t.Fatal("entry not found")
		}
		if string(entry.Content) != content {
			t.Fatalf("stored content = %s, want %s", entry.Content, content)
		}
		if string(entry.Details) != `{"k":1}` {
			t.Fatalf("stored details = %s", entry.Details)
		}

		projected := SessionEntryToContextMessages(entry)[0].(*ai.CustomMessage)
		var fields customMessageFields
		if err := json.Unmarshal(projected.Content, &fields); err != nil {
			t.Fatal(err)
		}
		if string(fields.Content) != content {
			t.Fatalf("projected content = %s, want %s", fields.Content, content)
		}
		if string(fields.Details) != `{"k":1}` {
			t.Fatalf("projected details = %s", fields.Details)
		}

		// Re-persisting the projection must not add another escaping layer.
		again := m.AppendCustomMessageEntry(fields.CustomType, fields.Content, fields.Display, fields.Details)
		for i := range m.GetEntries() {
			if m.GetEntries()[i].ID == again && string(m.GetEntries()[i].Content) != content {
				t.Fatalf("round-trip content = %s, want %s", m.GetEntries()[i].Content, content)
			}
		}
	}
}

// Upstream estimateTokens sums the system content chars, each section's length,
// and JSON.stringify(toolsAdded).length. The port used the rendered prompt text,
// omitting toolsAdded and counting the "\n\n" separators.
func TestSystemMessageTokenEstimateMatchesUpstream(t *testing.T) {
	section := "de"
	system := &ai.SystemMessage{Content: ai.StringOrBlocks{Text: "abc"}, Timestamp: 0}
	system.SetSection("s", &section)
	system.ToolsAdded = []ai.Tool{{Name: "t", Description: "d", Parameters: json.RawMessage(`{}`)}}

	toolsJSON, err := ai.MarshalJSON(system.ToolsAdded)
	if err != nil {
		t.Fatal(err)
	}
	want := ceilDiv(3+len(section)+JSLength(string(toolsJSON)), 4)
	if got := EstimateAgentMessageTokens(system); got != want {
		t.Fatalf("estimate = %d, want %d (content 3 + section %d + tools %d)", got, want, len(section), JSLength(string(toolsJSON)))
	}
}
