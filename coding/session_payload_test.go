package coding

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dat267/pier/ai"
)

// A resident session must keep its entries as index shells: the projection, a
// branch snapshot, a full snapshot and a single-entry read all hand out
// hydrated copies, so nothing quietly pins the payloads in the manager (D488).
func TestLoadedSessionKeepsPayloadsOutOfTheManager(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	lines := []string{
		`{"type":"session","version":3,"id":"s1","timestamp":"t0","cwd":"/tmp"}`,
		`{"type":"message","id":"u1","parentId":null,"timestamp":"t","message":{"role":"user","content":"first"}}`,
		`{"type":"message","id":"a1","parentId":"u1","timestamp":"t","message":{"role":"assistant","content":[{"type":"text","text":"answer"}]}}`,
		`{"type":"message","id":"u2","parentId":"a1","timestamp":"t","message":{"role":"user","content":"second"}}`,
	}
	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := OpenSession(path, dir, "")
	if err != nil {
		t.Fatal(err)
	}

	// Everything a running session reads in a turn.
	projection := manager.Projection()
	branch := manager.GetBranch("")
	entries := manager.GetEntries()
	one := manager.GetEntry("a1")

	if got := len(projection.Messages); got != 3 {
		t.Fatalf("projection messages = %d, want 3", got)
	}

	// A branch is a list of shells: callers that need bytes read them one at a
	// time. A full snapshot and a single-entry read hand out hydrated copies.
	for _, entry := range branch {
		if entry.Type == "message" && (!entry.lazy || len(entry.Message) != 0) {
			t.Fatalf("branch entry %s is not a shell: lazy=%v message=%d", entry.ID, entry.lazy, len(entry.Message))
		}
	}
	for _, entry := range entries {
		if entry.Type == "message" && len(entry.Message) == 0 {
			t.Fatalf("snapshot entry %s has no message", entry.ID)
		}
	}
	if one == nil || len(one.Message) == 0 {
		t.Fatal("GetEntry did not hydrate the entry")
	}
	if payload := manager.EntryMessage(&branch[1]); string(payload) != `{"role":"assistant","content":[{"type":"text","text":"answer"}]}` {
		t.Fatalf("EntryMessage = %s", payload)
	}

	// ...and the manager keeps none of them.
	manager.mu.Lock()
	defer manager.mu.Unlock()
	for _, fileEntry := range manager.fileEntries {
		entry := fileEntry.Entry
		if entry == nil || entry.Type != "message" {
			continue
		}
		if !entry.lazy || len(entry.Message) != 0 || len(entry.Raw()) != 0 {
			t.Fatalf("entry %s was hydrated in the manager: lazy=%v message=%d raw=%d",
				entry.ID, entry.lazy, len(entry.Message), len(entry.Raw()))
		}
	}
}

// A lazily loaded entry's payload is its file line: the projection must decode
// the same messages an in-memory session produces from the same entries.
func TestLazyPayloadProjectsTheSameMessages(t *testing.T) {
	persist := true
	source := NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: &persist})
	source.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}})
	source.AppendMessage(&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "test", Model: "m",
		Content: ai.ContentList{ai.TextContent{Text: "hi"}}, StopReason: ai.StopStop,
	})
	want := ""
	for _, message := range source.Projection().Messages {
		encoded, err := ai.MarshalJSON(message)
		if err != nil {
			t.Fatal(err)
		}
		want += string(encoded) + "\n"
	}

	reopened, err := OpenSession(source.GetSessionFile(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	got := ""
	for _, message := range reopened.Projection().Messages {
		encoded, err := ai.MarshalJSON(message)
		if err != nil {
			t.Fatal(err)
		}
		got += string(encoded) + "\n"
	}
	if got != want {
		t.Fatalf("projection differs:\n got %s\nwant %s", got, want)
	}
}
