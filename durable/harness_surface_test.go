package durable

import (
	"context"
	"testing"
)

// Cover the public harness surface end to end.

func TestHarnessConversationSurface(t *testing.T) {
	harness, storage := openTestHarness(t)
	ctx := context.Background()
	root, err := harness.Root(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Configure then resolve the agent.
	if err := root.Configure(ctx, AgentChange{Instructions: SetOf("be brief")}); err != nil {
		t.Fatal(err)
	}
	agent, err := root.Agent(ctx)
	if err != nil || agent.Instructions == nil || *agent.Instructions != "be brief" {
		t.Fatalf("agent = %+v, %v", agent, err)
	}
	// Commit an entry, then read it through entries and context.
	var entryID Id
	if err := root.Commit(ctx, func(tx *Transaction) error {
		entry, err := tx.AppendEntry(root.ID(), EntryDraft{Kind: "note"})
		if err != nil {
			return err
		}
		entryID = entry.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	page, err := root.Entries(ctx, EntryQuery{}, 10, nil)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != entryID {
		t.Fatalf("entries = %+v, %v", page.Items, err)
	}
	view, err := root.Context(ctx)
	if err != nil || len(view.Entries) != 1 {
		t.Fatalf("context = %+v, %v", view, err)
	}
	// Reset admits a pi.reset write.
	if err := root.Reset(ctx, stringPointer("handoff")); err != nil {
		t.Fatal(err)
	}
	// Compact admits a manual compaction task.
	taskID, err := root.Compact(ctx, nil)
	if err != nil || taskID == 0 {
		t.Fatalf("compact = %d, %v", taskID, err)
	}
	// Fork creates a conversation sharing history up to the entry.
	forked, err := root.Fork(ctx, entryID, ConversationCreateOptions{
		Ownership: ConversationOwnership{Kind: ConversationOwnerless},
	})
	if err != nil || forked.ID() == root.ID() {
		t.Fatalf("forked = %+v, %v", forked, err)
	}
	// View state and watch expose the structural view.
	state, err := root.ViewState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if value := state.Published(); value.Conversation.ID != root.ID() {
		t.Fatalf("view = %+v", value.Conversation)
	}
	watch, err := root.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	watch.Stop()
	// A conversation handle is retrievable by id.
	again, err := harness.Conversation(ctx, root.ID())
	if err != nil || again == nil || again.ID() != root.ID() {
		t.Fatalf("conversation = %+v, %v", again, err)
	}
	_ = storage
}

// Port of Conversation.context({ at }) from
// packages/durable/src/harness/harness.ts at pi v1.1.0 commit 76f6c06da.
func TestHarnessConversationContextAsOfEntry(t *testing.T) {
	harness, _ := openTestHarness(t)
	ctx := context.Background()
	root, err := harness.Root(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var first, second Id
	if err := root.Commit(ctx, func(tx *Transaction) error {
		var err error
		entry, err := tx.AppendEntry(root.ID(), EntryDraft{Kind: "note"})
		if err != nil {
			return err
		}
		first = entry.ID
		entry, err = tx.AppendEntry(root.ID(), EntryDraft{Kind: "note"})
		if err == nil {
			second = entry.ID
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	view, err := root.Context(ctx, ConversationContextOptions{At: &first})
	if err != nil || len(view.Entries) != 1 || view.Entries[0].ID != first {
		t.Fatalf("context as of %d = %+v, want entry %d only (second is %d), err=%v", first, view.Entries, first, second, err)
	}
}

func TestHarnessConversationContextRejectsInvisibleCutoff(t *testing.T) {
	harness, _ := openTestHarness(t)
	ctx := context.Background()
	root, err := harness.Root(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	invisible := Id(999)
	if _, err := root.Context(ctx, ConversationContextOptions{At: &invisible}); err == nil {
		t.Fatal("context cutoff rejects an entry outside the conversation")
	}
}

func TestHarnessTaskAndSubmissionSurface(t *testing.T) {
	harness, storage := openTestHarness(t)
	ctx := context.Background()
	root, err := harness.Root(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// GetTask reads a committed task; AbortTask marks it.
	var taskID Id
	if err := sessionCommit(t, harness.Session, func(tx *Transaction) error {
		created, err := CreateGeneration(tx, root.ID())
		taskID = created
		return err
	}); err != nil {
		t.Fatal(err)
	}
	record, err := harness.GetTask(ctx, taskID)
	if err != nil || record == nil || record.ID != taskID {
		t.Fatalf("getTask = %+v, %v", record, err)
	}
	if result, err := harness.AbortTask(taskID, ctx); err != nil || result != "marked" {
		t.Fatalf("abortTask = %q, %v", result, err)
	}
	// WaitForIdle resolves (the marked task is not reserved without a drain).
	if err := harness.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	// Usage sums the conversations' pi.usage (empty here).
	usage, err := harness.Usage(ctx)
	if err != nil || usage.Models == nil || usage.Tools == nil {
		t.Fatalf("usage = %+v, %v", usage, err)
	}
	// A submission handle is retrievable; an unknown id is not.
	submission, err := root.Submit(ctx, SubmissionDraft{Type: SubmissionTypeWrite, Entry: &EntryDraft{Kind: "note"}})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := harness.Submission(submission.ID(), ctx)
	if err != nil || handle == nil || handle.ID() != submission.ID() {
		t.Fatalf("submission = %+v, %v", handle, err)
	}
	if result, err := harness.AbortSubmission(999_999, ctx, nil); err != nil || result != "not_found" {
		t.Fatalf("abortSubmission = %q, %v", result, err)
	}
	// Inspect derives the scheduling state (WaitForIdle resumed it above, so
	// the marked task has already been aborted).
	inspection, err := harness.Inspect(ctx)
	if err != nil || (inspection.Scheduling != SchedulingRunning && inspection.Scheduling != SchedulingPaused) {
		t.Fatalf("inspection = %+v, %v", inspection, err)
	}
	// The task graph is available as an attached state and a watch.
	graph, err := harness.TaskGraph(ctx)
	if err != nil || graph == nil {
		t.Fatalf("taskGraph = %+v, %v", graph, err)
	}
	_ = graph.Published()
	taskWatch, err := harness.WatchTaskGraph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	taskWatch.Stop()
	_ = storage
}

// sessionCommit runs a plain session commit.
func sessionCommit(t *testing.T, session *Session, change func(tx *Transaction) error) error {
	t.Helper()
	return session.Commit(context.Background(), change)
}
