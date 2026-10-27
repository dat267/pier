package coding

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.4.0 (4a080252), sessionEntryToContextMessages returns the
// particular entry's message, never another entry's data with a matching ID.
func TestEntryMessagesCachesOnlyCanonicalPayloads(t *testing.T) {
	manager := NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: new(false)})
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "original"}, Timestamp: 1})
	entry := manager.GetEntries()[0]
	cached := manager.Projection().Messages[0]
	if got := manager.EntryMessages(&entry); len(got) != 1 || got[0] != cached {
		t.Fatal("a canonical descriptor snapshot did not share the cached message")
	}

	foreign := entry
	// The replacement has the same byte length, so a length-only guard is not
	// enough to distinguish an external descriptor from the cached source.
	foreign.Message = json.RawMessage(strings.Replace(string(entry.Message), "original", "modified", 1))
	for _, id := range []string{entry.ID, "other-session", ""} {
		foreign.ID = id
		messages := manager.EntryMessages(&foreign)
		if len(messages) != 1 || messages[0].(*ai.UserMessage).Content.Text != "modified" {
			t.Fatalf("external entry %q reused the wrong payload: %#v", id, messages)
		}
	}
	if messages := manager.EntryMessages(&entry); len(messages) != 1 || messages[0] != cached || messages[0].(*ai.UserMessage).Content.Text != "original" {
		t.Fatal("an external descriptor polluted the canonical cache")
	}

	custom := entry
	custom.Type, custom.CustomType, custom.Content = "custom_message", "note", json.RawMessage(`"custom"`)
	messages := manager.EntryMessages(&custom)
	if len(messages) != 1 {
		t.Fatalf("synthetic messages = %#v", messages)
	}
	if synthetic, ok := messages[0].(*ai.CustomMessage); !ok || synthetic.Role != "custom" {
		t.Fatalf("synthetic entry reused a message with the same ID: %#v", messages)
	}
	manager.NewSession(nil)
	if messages := manager.EntryMessages(&entry); len(messages) != 1 || messages[0] == cached || messages[0].(*ai.UserMessage).Content.Text != "original" {
		t.Fatal("a stale descriptor after reset reused the old manager cache")
	}
}
