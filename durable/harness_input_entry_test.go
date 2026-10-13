package durable

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// Port of harness-input-entries.test.ts's first case: the host-facing
// ConversationHandle submit takes an entry, so an input can be placed as an
// application entry carrying data instead of pi.user.
func TestInputSubmissionDraftPlacesEntry(t *testing.T) {
	harness, storage := openTestHarness(t)
	ctx := context.Background()
	if _, err := harness.Root(ctx, nil); err != nil {
		t.Fatal(err)
	}
	invocation := NewInvocation(0, RootConversationID, "run", context.Background())
	handle := &invocationConversation{
		id: RootConversationID, invocation: invocation, submissions: harness.submissions, tasks: harness.tasks,
	}
	submission, err := handle.Submit(InputSubmissionDraft{
		Content: "hello",
		Entry:   &InputEntry{Kind: "app.note", Data: map[string]any{"author": "me"}},
	}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	harness.Resume()
	done := make(chan struct{})
	go func() {
		_, _ = submission.Wait(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("submission did not settle")
	}
	page, err := storage.ScanEntries(ctx, EntryQuery{ConversationID: RootConversationID}, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range page.Items {
		if entry.Kind != "app.note" {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal(entry.Data, &data); err != nil || data["author"] != "me" {
			t.Fatalf("entry data = %s, %v", entry.Data, err)
		}
		return
	}
	t.Fatalf("no app.note entry: %+v", page.Items)
}

// Port of the reserved-kind rejection: a `pi.`-prefixed kind is refused.
func TestInputSubmissionDraftRejectsReservedEntryKind(t *testing.T) {
	harness, _ := openTestHarness(t)
	ctx := context.Background()
	if _, err := harness.Root(ctx, nil); err != nil {
		t.Fatal(err)
	}
	invocation := NewInvocation(0, RootConversationID, "run", context.Background())
	handle := &invocationConversation{
		id: RootConversationID, invocation: invocation, submissions: harness.submissions, tasks: harness.tasks,
	}
	if _, err := handle.Submit(InputSubmissionDraft{
		Content: "x", Entry: &InputEntry{Kind: "pi.note"},
	}, ctx); err == nil {
		t.Fatal("a pi.-prefixed entry kind was accepted")
	}
}
