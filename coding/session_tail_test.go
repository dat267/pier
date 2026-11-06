package coding

import (
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

// /tail is pier-only: upstream has no command that copies the live context
// into a fresh session (its newSession only exposes an extension setup hook, and
// extension mechanics are out of scope here, D41). The command must therefore
// keep the *projection* intact while dropping the cold prefix: same messages,
// same token estimate, fewer entries on disk.
func newTailSource(t *testing.T) (*SessionManager, *ai.Model) {
	t.Helper()
	persist := true
	source := NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: &persist})
	model := &ai.Model{ID: "m", Provider: "test", ContextWindow: 1000}
	source.AppendModelChange(model.Provider, model.ID)
	source.AppendThinkingLevelChange(string(ai.ThinkHigh))
	source.AppendMessage(&ai.SystemMessage{Content: ai.StringOrBlocks{Text: "system prompt"}, Timestamp: 1})
	source.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "cold question"}, Timestamp: 2})
	source.AppendMessage(&ai.AssistantMessage{
		API: ai.APIOpenAICompletions, Provider: model.Provider, Model: model.ID,
		Content: ai.ContentList{ai.TextContent{Text: "cold answer"}}, StopReason: ai.StopStop,
		Usage: ai.Usage{Input: 10, Output: 5, TotalTokens: 15}, Timestamp: 3,
	})
	kept := source.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "kept question"}, Timestamp: 4})
	source.AppendMessage(&ai.AssistantMessage{
		API: ai.APIOpenAICompletions, Provider: model.Provider, Model: model.ID,
		Content: ai.ContentList{ai.TextContent{Text: "kept answer"}}, StopReason: ai.StopStop,
		Usage: ai.Usage{Input: 20, Output: 5, TotalTokens: 25}, Timestamp: 5,
	})
	// A system message inside the kept range is not replayed there, and a tool
	// pair plus a context edit exercise the parts of the window that must travel
	// unchanged.
	source.AppendMessage(&ai.SystemMessage{Content: ai.StringOrBlocks{Text: "kept system note"}, Timestamp: 5})
	toolCall := source.AppendMessage(&ai.AssistantMessage{
		API: ai.APIOpenAICompletions, Provider: model.Provider, Model: model.ID,
		Content:    ai.ContentList{ai.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)}},
		StopReason: ai.StopToolUse, Usage: ai.Usage{Input: 5, Output: 2, TotalTokens: 7}, Timestamp: 5,
	})
	source.AppendMessage(&ai.ToolResultMessage{
		ToolCallID: "call-1", ToolName: "read",
		Content: ai.UserContentList{ai.TextContent{Text: "file contents"}}, Timestamp: 5,
	})
	if _, err := source.AppendContextEdit(toolCall, json.RawMessage(`{"content":"edited call"}`)); err != nil {
		t.Fatalf("context edit: %v", err)
	}
	source.AppendCompaction("summary of the cold prefix", kept, 25, nil, false, nil)
	source.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "live question"}, Timestamp: 6})
	source.AppendMessage(&ai.AssistantMessage{
		API: ai.APIOpenAICompletions, Provider: model.Provider, Model: model.ID,
		Content: ai.ContentList{ai.TextContent{Text: "live answer"}}, StopReason: ai.StopStop,
		Usage: ai.Usage{Input: 30, Output: 5, TotalTokens: 35}, Timestamp: 7,
	})
	return source, model
}

func projectionFingerprint(t *testing.T, manager *SessionManager) string {
	t.Helper()
	digest := ""
	for _, message := range manager.Projection().Messages {
		encoded, err := ai.MarshalJSON(message)
		if err != nil {
			t.Fatalf("marshal projected message: %v", err)
		}
		digest += string(encoded) + "\n"
	}
	return digest
}

func TestTailSessionKeepsTheLiveContext(t *testing.T) {
	source, model := newTailSource(t)
	sourceMessages := projectionFingerprint(t, source)
	sourceEstimate := source.ProjectedContextEstimate()
	sourceEntries := len(source.GetEntries())

	tailed, err := TailSession(source, model, ai.ThinkHigh, source.GetCwd(), source.GetSessionDir(), source.SessionDirManaged(), nil)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	if tailed.GetSessionFile() == "" || tailed.GetSessionFile() == source.GetSessionFile() {
		t.Fatalf("tail file = %q, want a new file", tailed.GetSessionFile())
	}
	if got := projectionFingerprint(t, tailed); got != sourceMessages {
		t.Fatalf("tail projection differs from the source:\n got %q\nwant %q", got, sourceMessages)
	}
	if got := tailed.ProjectedContextEstimate(); got.Tokens != sourceEstimate.Tokens {
		t.Fatalf("tail estimate = %d, want %d", got.Tokens, sourceEstimate.Tokens)
	}
	if got := len(tailed.GetEntries()); got >= sourceEntries {
		t.Fatalf("tail kept %d of %d entries; the cold prefix should be dropped", got, sourceEntries)
	}
	// The new session is a session-level fork of the source: the picker's
	// threaded view nests it under the parent, while the parent file keeps its own
	// history (the window here is re-rooted, so /tree shows only the window).
	header := tailed.GetHeader()
	if header == nil || header.ParentSession == nil || *header.ParentSession != source.GetSessionFile() {
		t.Fatalf("parentSession = %v, want %q", header, source.GetSessionFile())
	}
	for _, message := range tailed.Projection().Messages {
		if role := ai.RoleOf(message); role == ai.RoleUser {
			if text, ok := message.(*ai.UserMessage); ok && text.Content.String() && text.Content.Text == "cold question" {
				t.Fatal("tail carried the cold question")
			}
		}
	}
	// The model and thinking level travel with the file so a resume starts with
	// the same provider/model and reasoning effort.
	entries := tailed.GetEntries()
	if len(entries) == 0 || entries[0].Type != "model_change" || entries[0].ModelID != "m" {
		t.Fatalf("first entry = %+v, want a model_change for m", entriesIfAny(entries))
	}
	if entries[1].Type != "thinking_level_change" || entries[1].ThinkingLevel != string(ai.ThinkHigh) {
		t.Fatalf("second entry = %+v, want a thinking_level_change to high", entries[1])
	}

	// A resume must produce the same context from the file alone.
	reopened, err := OpenSession(tailed.GetSessionFile(), "", "")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := projectionFingerprint(t, reopened); got != sourceMessages {
		t.Fatalf("reopened projection differs:\n got %q\nwant %q", got, sourceMessages)
	}
}

func TestTailSessionWithoutCompactionCopiesTheBranch(t *testing.T) {
	persist := true
	source := NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: &persist})
	model := &ai.Model{ID: "m", Provider: "test", ContextWindow: 1000}
	source.AppendModelChange(model.Provider, model.ID)
	source.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}, Timestamp: 1})
	source.AppendMessage(&ai.AssistantMessage{
		API: ai.APIOpenAICompletions, Provider: model.Provider, Model: model.ID,
		Content: ai.ContentList{ai.TextContent{Text: "hi"}}, StopReason: ai.StopStop,
		Usage: ai.Usage{Input: 5, Output: 1, TotalTokens: 6}, Timestamp: 2,
	})
	want := projectionFingerprint(t, source)

	tailed, err := TailSession(source, model, ai.ThinkMedium, source.GetCwd(), source.GetSessionDir(), source.SessionDirManaged(), nil)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	if got := projectionFingerprint(t, tailed); got != want {
		t.Fatalf("tail projection differs:\n got %q\nwant %q", got, want)
	}
}

func TestTailSessionNeedsAConversation(t *testing.T) {
	persist := true
	source := NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: &persist})
	if _, err := TailSession(source, nil, "", source.GetCwd(), source.GetSessionDir(), source.SessionDirManaged(), nil); err == nil {
		t.Fatal("tail of an empty session succeeded")
	}
	// A fresh app session already carries model/thinking entries; bookkeeping
	// alone is still not a conversation.
	source.AppendModelChange("test", "m")
	source.AppendThinkingLevelChange("high")
	source.AppendMessage(&ai.SystemMessage{Content: ai.StringOrBlocks{Text: "system prompt"}, Timestamp: 1})
	if _, err := TailSession(source, nil, "", source.GetCwd(), source.GetSessionDir(), source.SessionDirManaged(), nil); err == nil {
		t.Fatal("tail of a session with only bookkeeping and its system prompt succeeded")
	}
}

func entriesIfAny(entries []SessionEntry) any {
	if len(entries) == 0 {
		return nil
	}
	first := entries[0]
	first.Message = json.RawMessage(nil)
	return first
}

// TailSession's own refusals carry the same wording as the command, so the
// message does not name the command /tail replaced.
func TestTailSessionRefusalsNameTail(t *testing.T) {
	if _, err := TailSession(nil, nil, "", "", "", false, nil); err == nil || err.Error() != "Nothing to tail yet" {
		t.Errorf("nil source error = %v, want \"Nothing to tail yet\"", err)
	}
	source := NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: new(false)})
	if _, err := TailSession(source, nil, "", "", "", false, nil); err == nil || err.Error() != "Nothing to tail yet" {
		t.Errorf("empty branch error = %v, want \"Nothing to tail yet\"", err)
	}
}

// A session loaded from disk carries its original JSON lines. Mutating a loaded
// entry's parent field alone does not reach that line, so the copied chain must
// re-chain through the raw helper; otherwise the first copied entry keeps a
// parent outside the window and the kept range stops being reachable from the
// leaf.
func TestTailSessionKeepsTheLiveContextOnAReopenedSession(t *testing.T) {
	source, model := newTailSource(t)
	reopened, err := OpenSession(source.GetSessionFile(), "", "")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	before := projectionFingerprint(t, reopened)
	beforeBranch := len(reopened.GetBranch(""))
	beforeEntries := len(reopened.GetEntries())

	tailed, err := TailSession(reopened, model, ai.ThinkHigh, reopened.GetCwd(), reopened.GetSessionDir(), true, nil)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	if got := projectionFingerprint(t, tailed); got != before {
		t.Errorf("tail context differs:\n got %q\nwant %q", got, before)
	}
	branch := tailed.GetBranch("")
	if len(branch) != len(tailed.GetEntries()) {
		t.Errorf("leaf path has %d of %d copied entries; the chain is broken", len(branch), len(tailed.GetEntries()))
	}
	if len(branch) > 0 {
		if branch[0].ParentID != nil {
			t.Errorf("first copied entry still points at %q", *branch[0].ParentID)
		}
		for i := 1; i < len(branch); i++ {
			if branch[i].ParentID == nil || *branch[i].ParentID != branch[i-1].ID {
				t.Fatalf("entry %d does not chain to its predecessor", i)
			}
		}
	}
	if len(branch) >= beforeBranch || len(tailed.GetEntries()) >= beforeEntries {
		t.Errorf("tail did not drop the compacted prefix: %d/%d kept of %d/%d", len(branch), len(tailed.GetEntries()), beforeBranch, beforeEntries)
	}
}
