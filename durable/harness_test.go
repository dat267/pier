package durable

import (
	"context"
	"testing"
	"time"
)

// Port of the durable harness composition.

func openTestHarness(t *testing.T) (*Harness, Storage) {
	t.Helper()
	storage := NewMemoryStorage()
	// Pin fixture timestamps; tests that need a changing clock pass one explicitly.
	harness, err := OpenHarness(storage, HarnessOptions{Registry: NewRegistry(), Now: func() int64 { return 0 }}, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return harness, storage
}

func TestHarnessRootAndAgent(t *testing.T) {
	harness, _ := openTestHarness(t)
	ctx := context.Background()
	root, err := harness.Root(ctx, nil)
	if err != nil || root.ID() != RootConversationID {
		t.Fatalf("root = %+v, %v", root, err)
	}
	agent, err := root.Agent(ctx)
	if err != nil || agent.ThinkingLevel != "off" {
		t.Fatalf("agent = %+v, %v", agent, err)
	}
	// Root is idempotent.
	second, err := harness.Root(ctx, nil)
	if err != nil || second.ID() != root.ID() {
		t.Fatalf("root = %+v, %v", second, err)
	}
	if err := harness.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.Root(ctx, nil); err == nil {
		t.Fatal("a closed harness rejects")
	}
}

func TestHarnessCreateAndFork(t *testing.T) {
	harness, storage := openTestHarness(t)
	ctx := context.Background()
	root, err := harness.Root(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var entryID Id
	if err := harness.Session.Commit(ctx, func(tx *Transaction) error {
		entry, err := tx.AppendEntry(root.ID(), EntryDraft{Kind: "note"})
		if err != nil {
			return err
		}
		entryID = entry.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	created, err := harness.CreateConversation(ctx, ConversationCreateOptions{
		Ownership: ConversationOwnership{Kind: ConversationOwnerless},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID() == RootConversationID {
		t.Fatal("a new conversation has its own id")
	}
	record, err := storage.Conversation(ctx, created.ID())
	if err != nil || record == nil {
		t.Fatalf("record = %+v, %v", record, err)
	}
	forked, err := root.Fork(ctx, entryID, ConversationCreateOptions{Ownership: ConversationOwnership{Kind: ConversationOwnerless}})
	if err != nil || forked.ID() == root.ID() {
		t.Fatalf("forked = %+v, %v", forked, err)
	}
}

func TestHarnessSubmitAndWait(t *testing.T) {
	harness, _ := openTestHarness(t)
	ctx := context.Background()
	root, err := harness.Root(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	submission, err := root.Submit(ctx, SubmissionDraft{Type: SubmissionTypeInput, Content: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	harness.Resume()
	waited := make(chan SubmissionRecord, 1)
	errs := make(chan error, 1)
	go func() {
		record, err := submission.Wait(ctx)
		if err != nil {
			errs <- err
			return
		}
		waited <- record
	}()
	select {
	case record := <-waited:
		if record.Status != SubmissionUnanswered || record.Reason == nil || *record.Reason != "no_model" {
			t.Fatalf("record = %+v", record)
		}
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("submission did not settle")
	}
	usage, err := harness.Usage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if usage.Models == nil || usage.Tools == nil {
		t.Fatalf("usage = %+v", usage)
	}
	inspection, err := harness.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Tasks) != 0 {
		t.Fatalf("tasks = %+v", inspection.Tasks)
	}
}
