package coding

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/agent-session.ts dispose aborts manual and
// automatic compaction before disconnecting the session. A host may dispose
// from compaction_start; no summary may commit after that cancellation.
func TestDisposeCancelsActiveCompaction(t *testing.T) {
	for _, manual := range []bool{false, true} {
		name := "automatic"
		if manual {
			name = "manual"
		}
		t.Run(name, func(t *testing.T) {
			var runs atomic.Int64
			session := compactionTestSession(t, &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}, summarizingStreamFn(t, &runs, "summary text long enough to pass the usability check"), smallCompactionSettings())
			seedBranch(t, session)
			session.Subscribe(func(event *SessionEvent) {
				if event.Type == SessionCompactionStart {
					session.Dispose()
				}
			})
			var err error
			if manual {
				_, err = session.CompactSession(context.Background(), "")
			} else {
				_, err = session.RunAutoCompaction(context.Background(), CompactionThreshold, false)
			}
			if err == nil {
				t.Fatal("disposed session completed compaction instead of canceling it")
			}
			for _, entry := range session.Sessions.GetEntries() {
				if entry.Type == "compaction" {
					t.Fatal("disposed session persisted a compaction")
				}
			}
		})
	}
}

// The same dispose boundary owns branch-summary cancellation, not just the
// low-level agent run (core/agent-session.ts dispose -> abortBranchSummary).
func TestDisposeCancelsActiveBranchSummary(t *testing.T) {
	manager := sessionWithBranch(t)
	rootID := manager.GetEntries()[0].ID
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Sessions: manager, StreamFn: stubStreamFn,
		Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192},
	})
	if err != nil {
		t.Fatal(err)
	}
	var runs atomic.Int64
	summary := summarizingStreamFn(t, &runs, "branch summary text long enough to pass the usability check")
	session.CompactionStreamFn = func(model *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		session.Dispose()
		return summary(model, transcript, options)
	}
	result, err := session.NavigateTree(context.Background(), rootID, NavigateTreeOptions{Summarize: true})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.Aborted || !result.Cancelled {
		t.Fatalf("disposed branch summarization = %+v, want aborted and cancelled", result)
	}
	for _, entry := range manager.GetEntries() {
		if entry.Type == "branch_summary" {
			t.Fatal("disposed session persisted a branch summary")
		}
	}
}
