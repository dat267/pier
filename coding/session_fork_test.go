package coding

import (
	"context"
	"encoding/json"
	"github.com/dat267/pier/ai"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"weak"
)

func TestForkSessionCopiesEntriesUnderNewID(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	sourceCwd := t.TempDir()
	targetCwd := t.TempDir()
	source := NewSessionManager(sourceCwd, nil)
	source.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}})
	source.AppendMessage(&ai.AssistantMessage{
		API:        ai.APIAnthropicMessages,
		Provider:   "anthropic",
		Model:      "m",
		Content:    ai.ContentList{ai.TextContent{Text: "hi"}},
		StopReason: ai.StopStop,
	})
	sourcePath := source.GetSessionFile()

	forked, err := ForkSession(sourcePath, targetCwd, "", nil)
	if err != nil {
		t.Fatalf("ForkSession: %v", err)
	}
	if forked.GetSessionID() == source.GetSessionID() {
		t.Fatal("forked session must have a new id")
	}
	if forked.GetCwd() != targetCwd {
		t.Fatalf("forked cwd %s, want %s", forked.GetCwd(), targetCwd)
	}
	header := forked.GetHeader()
	if header == nil || header.ParentSession == nil || *header.ParentSession != sourcePath {
		t.Fatalf("forked header parentSession %+v, want %s", header, sourcePath)
	}
	// The forked file lives in the target cwd's default session dir.
	if want := DefaultSessionDir(targetCwd, ""); forked.GetSessionDir() != want {
		t.Fatalf("forked dir %s, want %s", forked.GetSessionDir(), want)
	}
	// The source file is untouched and still has its own entries.
	if _, err := os.Stat(sourcePath); err != nil {
		t.Fatalf("source file missing: %v", err)
	}
	if got := len(forked.BuildContextEntriesForLeaf()); got != 2 {
		t.Fatalf("forked entry count %d, want 2 (user + assistant)", got)
	}
}

func TestForkSessionWithExplicitID(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	source := NewSessionManager(t.TempDir(), nil)
	source.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}})
	source.AppendMessage(&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
		Content: ai.ContentList{ai.TextContent{Text: "hi"}}, StopReason: ai.StopStop,
	})

	forked, err := ForkSession(source.GetSessionFile(), t.TempDir(), "", &NewSessionOptions{ID: "019463a7-f000-7000-8000-00000000000f"})
	if err != nil {
		t.Fatalf("ForkSession: %v", err)
	}
	if forked.GetSessionID() != "019463a7-f000-7000-8000-00000000000f" {
		t.Fatalf("forked id %s, want the explicit id", forked.GetSessionID())
	}
	if !strings.HasSuffix(forked.GetSessionFile(), "019463a7-f000-7000-8000-00000000000f.jsonl") {
		t.Fatalf("forked file %s should carry the explicit id", forked.GetSessionFile())
	}
}

func TestForkSessionInvalidIDRejected(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	source := NewSessionManager(t.TempDir(), nil)
	source.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}})
	source.AppendMessage(&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
		Content: ai.ContentList{ai.TextContent{Text: "hi"}}, StopReason: ai.StopStop,
	})

	if _, err := ForkSession(source.GetSessionFile(), t.TempDir(), "", &NewSessionOptions{ID: "bad id!"}); err == nil {
		t.Fatal("expected an error for an invalid fork id")
	}
}

func TestForkSessionRejectsExplicitEmptyID(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	source := NewSessionManager(t.TempDir(), nil)
	source.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}})
	if _, err := ForkSession(source.GetSessionFile(), t.TempDir(), "", &NewSessionOptions{ID: "", IDSet: true}); err == nil {
		t.Fatal("expected an error for an explicitly empty fork id")
	}
}

func TestForkSessionEmptySourceRejected(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	if _, err := ForkSession("/nonexistent/session.jsonl", t.TempDir(), "", nil); err == nil {
		t.Fatal("expected an error for a missing/empty source")
	}
}

// Reference: pi v1.4.0 core/agent-session.ts getUserMessagesForForking uses
// contentText(content, ""). Prompt builds block content even without images.
func TestForkSelectorIncludesSubmittedPrompt(t *testing.T) {
	session := newPromptSession(t, &ai.Model{ID: "m", Provider: "anthropic", API: ai.APIAnthropicMessages, ContextWindow: 100000}, mockSessionStreamFn(createAssistantMessageT("reply")))
	if err := session.Prompt(context.Background(), "fork this prompt", nil); err != nil {
		t.Fatal(err)
	}
	forks := session.GetUserMessagesForForking()
	if len(forks) != 1 || forks[0].Text != "fork this prompt" || forks[0].EntryID == "" {
		t.Fatalf("fork messages = %+v; want the submitted prompt", forks)
	}
}

// Reference: pi v1.4.0 core/session-manager.ts forkFrom appends one newline
// per JSON.stringify(entry). Extra blank lines violate the JSONL byte contract.
func TestForkSessionWritesExactlyOneNewlinePerEntry(t *testing.T) {
	tempAgentDir(t)
	source := NewSessionManager(t.TempDir(), nil)
	source.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "question"}})
	source.AppendMessage(&ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "answer"}}, StopReason: ai.StopStop})
	for _, branch := range []bool{false, true} {
		name := "whole"
		if branch {
			name = "branch"
		}
		t.Run(name, func(t *testing.T) {
			var fork *SessionManager
			var err error
			if branch {
				fork, err = ForkSessionAtEntry(source, *source.GetLeafID(), source.GetCwd(), t.TempDir(), nil)
			} else {
				fork, err = ForkSession(source.GetSessionFile(), source.GetCwd(), t.TempDir(), nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			data := readFileBytes(t, fork.GetSessionFile())
			if got, want := strings.Count(string(data), "\n"), len(fork.GetEntries())+1; got != want {
				t.Fatalf("JSONL newlines=%d want=%d", got, want)
			}
		})
	}
}

// Reference: session-manager.ts propagates JSON.stringify failures instead of
// silently omitting an entry from the fork's transcript.
func TestForkSessionRejectsUnserializableEntries(t *testing.T) {
	persist := false
	source := NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: &persist})
	source.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "question"}})
	id := source.AppendCustomEntry("broken", json.RawMessage(`{`))
	if fork, err := ForkSessionAtEntry(source, id, source.GetCwd(), t.TempDir(), nil); err == nil {
		t.Fatalf("invalid entry was silently dropped; fork=%v", fork.GetEntries())
	}
}

// Reference: pi v1.4.0 (4a080252), core/session-manager.ts
// createBranchedSession captures previousSessionFile before replacing it.
func TestBranchedSessionRecordsItsSourceFile(t *testing.T) {
	for _, persist := range []bool{false, true} {
		name := "memory"
		if persist {
			name = "file"
		}
		t.Run(name, func(t *testing.T) {
			m := NewSessionManager(t.TempDir(), &SessionManagerOptions{SessionDir: t.TempDir(), Persist: &persist})
			id := m.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "keep"}, Timestamp: 1})
			source := m.GetSessionFile()
			path, err := m.CreateBranchedSession(id)
			if err != nil {
				t.Fatal(err)
			}
			check := func(manager *SessionManager) {
				t.Helper()
				h := manager.GetHeader()
				if !persist {
					if h.ParentSession != nil {
						t.Fatalf("memory branch has parent %q", *h.ParentSession)
					}
					return
				}
				if h.ParentSession == nil {
					t.Fatal("fork has no parentSession")
				}
				if *h.ParentSession != source {
					t.Fatalf("parentSession = %q, want source %q", *h.ParentSession, source)
				}
			}
			check(m)
			if persist {
				if path == source {
					t.Fatal("fork overwrote source")
				}
				reopened, err := OpenSession(path, "", "")
				if err != nil {
					t.Fatal(err)
				}
				check(reopened)
			}
		})
	}
}

// Run the cyclic-file traversal in a child so a pre-fix infinite walk is
// bounded without leaving a spinning goroutine in the test process.
func TestSessionParentCycleDoesNotHang(t *testing.T) {
	if os.Getenv("PIER_TEST_SESSION_CYCLE_CHILD") == "1" {
		manager, err := OpenSession(os.Getenv("PIER_TEST_SESSION_CYCLE_FILE"), "", "")
		if err != nil {
			t.Fatal(err)
		}
		branch := manager.GetBranch("")
		if len(branch) != 2 || branch[0].ID != "m3" || branch[1].ID != "m4" {
			t.Fatalf("cycle branch = %+v, want each entry once in root-to-leaf order", branch)
		}
		entries := manager.GetEntries()
		leafID := "m2"
		path := buildSessionPath(entries, &leafID, nil)
		if len(path) != 2 || path[0].ID != "m1" || path[1].ID != "m2" {
			t.Fatalf("cycle path = %+v, want each entry once in root-to-leaf order", path)
		}
		collected := CollectEntriesForBranchSummary(manager, "m2", "m4")
		if collected.HasAncestor || len(collected.Entries) != 2 {
			t.Fatalf("disconnected-cycle collection = %+v, want two entries without an ancestor", collected)
		}
		return
	}

	path := filepath.Join(t.TempDir(), "cycle.jsonl")
	const content = `{"type":"session","version":3,"id":"s","timestamp":"t","cwd":"/tmp"}` + "\n" +
		`{"type":"message","id":"m1","parentId":"m2","timestamp":"t","message":{"role":"user","content":"x"}}` + "\n" +
		`{"type":"message","id":"m2","parentId":"m1","timestamp":"t","message":{"role":"assistant","content":"y"}}` + "\n" +
		`{"type":"message","id":"m3","parentId":"m4","timestamp":"t","message":{"role":"user","content":"z"}}` + "\n" +
		`{"type":"message","id":"m4","parentId":"m3","timestamp":"t","message":{"role":"assistant","content":"w"}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSessionParentCycleDoesNotHang$")
	cmd.Env = append(os.Environ(), "PIER_TEST_SESSION_CYCLE_CHILD=1", "PIER_TEST_SESSION_CYCLE_FILE="+path)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("parent-cycle traversal did not terminate: %s", output)
	}
	if err != nil {
		t.Fatalf("child traversal failed: %v\n%s", err, output)
	}
}

// Reference: pi v1.4.0 (4a080252), core/session-manager.ts
// createBranchedSession recreates resolved bookmarks after the retained path.
func TestBranchedSessionPreservesBookmarks(t *testing.T) {
	for _, persist := range []bool{false, true} {
		name := "memory"
		if persist {
			name = "file"
		}
		t.Run(name, func(t *testing.T) {
			m := NewSessionManager(t.TempDir(), &SessionManagerOptions{SessionDir: t.TempDir(), Persist: &persist})
			user := m.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "keep"}, Timestamp: 1})
			label := "bookmark"
			oldLabel, err := m.AppendLabelChange(user, &label)
			if err != nil {
				t.Fatal(err)
			}
			timestamp := m.GetEntry(oldLabel).Timestamp
			leaf := m.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "next"}, Timestamp: 2})
			if m.GetLabel(user) != label {
				t.Fatal("control: original bookmark missing")
			}
			path, err := m.CreateBranchedSession(leaf)
			if err != nil {
				t.Fatal(err)
			}
			check := func(manager *SessionManager) {
				t.Helper()
				if got := manager.GetLabel(user); got != label {
					t.Fatalf("bookmark = %q, want %q", got, label)
				}
				entries := manager.GetEntries()
				if len(entries) != 3 {
					t.Fatalf("entries = %d, want two messages and bookmark", len(entries))
				}
				entry := entries[2]
				if entry.Type != "label" || entry.TargetID != user || entry.ID == oldLabel || entry.ParentID == nil || *entry.ParentID != leaf || entry.Timestamp != timestamp {
					t.Fatalf("recreated bookmark = %+v", entry)
				}
				if manager.GetEntry(oldLabel) != nil {
					t.Fatal("original label entry survived")
				}
			}
			check(m)
			if persist {
				reopened, err := OpenSession(path, "", "")
				if err != nil {
					t.Fatal(err)
				}
				check(reopened)
			}
		})
	}
}

// pi v1.4.0 (4a080252), core/session-manager.ts createBranchedSession
// replaces fileEntries and rebuilds byId from only the retained path. The
// shallow entry copies preserve message identity on that path, but discarded
// messages must not remain owned by the manager's port-local read caches.
func TestCreateBranchedSessionReleasesDiscardedMessages(t *testing.T) {
	for _, reader := range []string{"memo", "signature", "branch", "projection"} {
		t.Run(reader, func(t *testing.T) {
			manager := InMemorySession(t.TempDir(), nil, nil)
			keptID := manager.AppendMessage(createUserMessage("keep"))
			kept := manager.EntryMessages(manager.GetEntry(keptID))[0]
			discardedID, raw, decoded := cacheDiscardedBranchMessage(manager, reader)
			if _, err := manager.CreateBranchedSession(keptID); err != nil {
				t.Fatal(err)
			}
			if manager.GetEntry(discardedID) != nil {
				t.Fatal("fork still contains the discarded entry")
			}
			runtime.GC()
			if raw.Value() != nil {
				t.Error("fork retains discarded raw message bytes")
			}
			if decoded.Value() != nil {
				t.Error("fork retains discarded decoded message")
			}
			if got := manager.EntryMessages(manager.GetEntry(keptID))[0]; got != kept {
				t.Error("fork changed the retained message's identity")
			}
			runtime.KeepAlive(manager)
		})
	}
}

func cacheDiscardedBranchMessage(manager *SessionManager, reader string) (string, weak.Pointer[byte], weak.Pointer[ai.UserMessage]) {
	id := manager.AppendMessage(createUserMessage(strings.Repeat("discard", 1024)))
	entry := manager.GetEntry(id)
	message := manager.EntryMessages(entry)[0].(*ai.UserMessage)
	switch reader {
	case "signature":
		manager.ContextSignature()
	case "branch":
		manager.GetBranch("")
	case "projection":
		manager.Projection()
	}
	return id, weak.Make(&entry.Message[0]), weak.Make(message)
}

// Reference: pi v1.4.0 (4a080252), core/session-manager.ts
// createBranchedSession remaps removed labels to the next retained entry.
func TestBranchedSessionKeepsCompactionWindow(t *testing.T) {
	for _, persist := range []bool{false, true} {
		for _, target := range []string{"label", "message", "self"} {
			name := "memory/"
			if persist {
				name = "file/"
			}
			t.Run(name+target, func(t *testing.T) {
				m := NewSessionManager(t.TempDir(), &SessionManagerOptions{SessionDir: t.TempDir(), Persist: &persist})
				old := m.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "summarized"}, Timestamp: 1})
				bookmark := "bookmark"
				label, err := m.AppendLabelChange(old, &bookmark)
				if err != nil {
					t.Fatal(err)
				}
				kept := m.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "keep"}, Timestamp: 2})
				first := label
				switch target {
				case "message":
					first = kept
				case "self":
					first = ""
				}
				compaction := m.AppendCompaction("summary", first, 10, nil, false, nil)
				wantFirst := kept
				wantMessages := 2
				if target == "self" {
					wantFirst = compaction
					wantMessages = 1
				}
				if got := len(m.Projection().Messages); got != wantMessages {
					t.Fatalf("control messages = %d, want %d", got, wantMessages)
				}
				path, err := m.CreateBranchedSession(compaction)
				if err != nil {
					t.Fatal(err)
				}
				check := func(manager *SessionManager) {
					t.Helper()
					if got := manager.GetEntry(compaction).FirstKeptEntryID; got != wantFirst {
						t.Errorf("firstKeptEntryId = %q, want %q", got, wantFirst)
					}
					messages := manager.Projection().Messages
					if len(messages) != wantMessages {
						t.Fatalf("fork projection has %d messages, want %d", len(messages), wantMessages)
					}
					if wantMessages == 2 {
						user, ok := messages[1].(*ai.UserMessage)
						if !ok || user.Content.Text != "keep" {
							t.Fatalf("retained message = %#v", messages[1])
						}
					}
				}
				check(m)
				if persist {
					reopened, err := OpenSession(path, "", "")
					if err != nil {
						t.Fatal(err)
					}
					check(reopened)
				}
			})
		}
	}
}
