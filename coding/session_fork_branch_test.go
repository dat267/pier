package coding

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// A branch fork's leaf is the entry it was taken at. That is the whole point:
// the session format stores no leaf marker, so a session's leaf is its last
// entry — which is why the fork copies the *path* to the chosen entry rather
// than every entry.
func TestForkSessionAtEntryKeepsTheChosenLeaf(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	cwd := t.TempDir()
	source := NewSessionManager(cwd, nil)
	source.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "first"}})
	source.AppendMessage(&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
		Content: ai.ContentList{ai.TextContent{Text: "hi"}}, StopReason: ai.StopStop,
	})
	source.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "second"}})

	entries := source.GetEntries()
	if len(entries) != 3 {
		t.Fatalf("source entries = %d, want 3", len(entries))
	}
	firstUserID := entries[0].ID

	t.Run("at the first message", func(t *testing.T) {
		forked, err := ForkSessionAtEntry(source, firstUserID, cwd, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if leaf := forked.GetLeafID(); leaf == nil || *leaf != firstUserID {
			t.Errorf("leaf = %v, want %q", leaf, firstUserID)
		}
		if got := len(forked.GetEntries()); got != 1 {
			t.Errorf("entries = %d, want the single entry at the fork point", got)
		}
		// The fork records the source file it came from (parentSession), which
		// the session picker's tree and resume lineage read.
		forkedData, readErr := os.ReadFile(forked.GetSessionFile())
		if readErr != nil {
			t.Fatal(readErr)
		}
		firstLine, _, _ := strings.Cut(string(forkedData), "\n")
		var forkedHeader SessionHeader
		if err := json.Unmarshal([]byte(firstLine), &forkedHeader); err != nil {
			t.Fatalf("forked header %q: %v", firstLine, err)
		}
		if forkedHeader.ParentSession == nil || *forkedHeader.ParentSession != source.GetSessionFile() {
			t.Errorf("forked parentSession = %v, want %q", forkedHeader.ParentSession, source.GetSessionFile())
		}
		if forked.GetSessionID() == source.GetSessionID() {
			t.Error("the fork kept the source session id")
		}
	})

	t.Run("at the current leaf keeps everything", func(t *testing.T) {
		leaf := source.GetLeafID()
		forked, err := ForkSessionAtEntry(source, *leaf, cwd, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(forked.GetEntries()); got != 3 {
			t.Errorf("entries = %d, want 3", got)
		}
		if forked.GetLeafID() == nil || *forked.GetLeafID() != entries[2].ID {
			t.Errorf("leaf = %v, want the last entry", forked.GetLeafID())
		}
	})

	t.Run("an empty target forks an empty session", func(t *testing.T) {
		forked, err := ForkSessionAtEntry(source, "", cwd, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(forked.GetEntries()); got != 0 {
			t.Errorf("entries = %d, want none", got)
		}
		if forked.GetLeafID() != nil {
			t.Errorf("leaf = %v, want none", forked.GetLeafID())
		}
	})

	t.Run("an unknown entry is rejected", func(t *testing.T) {
		if _, err := ForkSessionAtEntry(source, "no-such-entry", cwd, "", nil); err == nil {
			t.Error("an unknown entry id should be an error")
		}
	})

	// The source is untouched.
	if got := len(source.GetEntries()); got != 3 {
		t.Errorf("source entries = %d, want 3", got)
	}
}

// CreateBranchedSession holds the manager lock and must use the locked branch
// helper: calling the locking GetBranch from inside deadlocked the persisted
// RPC fork/clone path.
func TestCreateBranchedSessionBranchesInPlace(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	source := NewSessionManager(t.TempDir(), &SessionManagerOptions{SessionDir: t.TempDir()})
	source.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "first"}})
	source.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "second"}})
	entries := source.GetEntries()
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}

	type branchResult struct {
		path string
		err  error
	}
	done := make(chan branchResult, 1)
	go func() {
		path, err := source.CreateBranchedSession(entries[0].ID)
		done <- branchResult{path, err}
	}()
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.path == "" {
			t.Fatal("a persisted branch should have a session file")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CreateBranchedSession deadlocked")
	}
	if got := len(source.GetEntries()); got != 1 {
		t.Fatalf("branch entries = %d, want 1", got)
	}
	if leaf := source.GetLeafID(); leaf == nil || *leaf != entries[0].ID {
		t.Fatalf("branch leaf = %v, want %q", leaf, entries[0].ID)
	}
}

// A branched session records the file it came from in its header, which is what
// the session picker's tree and the resume lineage read. The condition is
// `parentSession != ""`, so a source that has a file must always carry it.
func TestCreateBranchedSessionRecordsTheParentFile(t *testing.T) {
	source := NewSessionManager(t.TempDir(), &SessionManagerOptions{SessionDir: t.TempDir()})
	source.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "first"}})
	original := source.GetSessionFile()
	if original == "" {
		t.Fatal("source session has no file")
	}
	entry := source.GetEntries()[0]
	path, err := source.CreateBranchedSession(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	firstLine, _, _ := strings.Cut(string(data), "\n")
	var header SessionHeader
	if err := json.Unmarshal([]byte(firstLine), &header); err != nil {
		t.Fatalf("header line %q: %v", firstLine, err)
	}
	if header.ParentSession == nil || *header.ParentSession != original {
		t.Fatalf("parentSession = %v, want %q", header.ParentSession, original)
	}
}
