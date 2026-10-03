package durable

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// Port of harness/submissions.ts.

func defaultQueueModes() QueueModes {
	return QueueModes{SteeringMode: QueueOneAtATime, FollowUpMode: QueueOneAtATime}
}

func newTestSubmissions(session *Session, storage Storage) *Submissions {
	return NewSubmissions(session, storage, func() int64 { return 100 }, defaultQueueModes, func() {})
}

func newRootSession(t *testing.T) (*Session, Storage) {
	t.Helper()
	storage := NewMemoryStorage()
	session := NewSession(storage)
	if err := session.Commit(context.Background(), func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return session, storage
}

func TestAdmitIdleInputStartsRun(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	var id Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		id, err = AdmitSubmission(tx, RootConversationID, SubmissionDraft{Type: SubmissionTypeInput, Content: "hello"}, 100, defaultQueueModes())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	record, _ := storage.Submission(ctx, id)
	if record == nil || record.Status != SubmissionPlaced || record.Type != SubmissionTypeInput || record.Entry == nil {
		t.Fatalf("record = %+v", record)
	}
	live, ok, _ := session.Snapshot(ctx, LiveDoc.Definition, RootConversationID)
	run, _ := live.(map[string]any)["run"].(map[string]any)
	if !ok || run == nil || run["taskId"] == nil {
		t.Fatalf("run = %+v", run)
	}
	inputs, _ := run["inputs"].([]any)
	if len(inputs) != 1 || !jsonIDEquals(inputs[0], id) {
		t.Fatalf("inputs = %+v", inputs)
	}
	// The placed input appended a user entry.
	page, _ := storage.ScanEntries(ctx, EntryQuery{ConversationID: RootConversationID}, nil, 10)
	found := false
	for _, entry := range page.Items {
		if entry.Kind == UserEntry.Kind {
			if message, ok := entry.Model[0].(*ai.UserMessage); ok && message.Content.Text == "hello" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("entries = %+v", page.Items)
	}
}

func TestAdmitBusyQueuesAndRejects(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		stageLive(t, tx, RootConversationID, "run", map[string]any{"taskId": 99.0, "inputs": []any{}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var id Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		id, err = AdmitSubmission(tx, RootConversationID, SubmissionDraft{Type: SubmissionTypeInput, Content: "x"}, 100, defaultQueueModes())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	record, _ := storage.Submission(ctx, id)
	if record.Status != SubmissionQueued {
		t.Fatalf("record = %+v", record)
	}
	items := readInbox(t, session, RootConversationID).Items
	if len(items) != 1 || items[0].ID != id || items[0].Mode != InboxFollowUp {
		t.Fatalf("items = %+v", items)
	}
	// A rejecting input writes nothing and reports the busy conversation.
	err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := AdmitSubmission(tx, RootConversationID, SubmissionDraft{
			Type: SubmissionTypeInput, Content: "y", WhenBusy: stringPointer("reject"),
		}, 100, defaultQueueModes())
		return err
	})
	var busy *ConversationBusyError
	if !errors.As(err, &busy) || busy.ConversationID != RootConversationID {
		t.Fatalf("err = %v", err)
	}
}

func TestAdmitRequestIDDeduplicates(t *testing.T) {
	session, _ := newRootSession(t)
	ctx := context.Background()
	request := "r1"
	var first Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		first, err = AdmitSubmission(tx, RootConversationID, SubmissionDraft{
			RequestID: &request, Type: SubmissionTypeInput, Content: "x",
		}, 100, defaultQueueModes())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var second Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		second, err = AdmitSubmission(tx, RootConversationID, SubmissionDraft{
			RequestID: &request, Type: SubmissionTypeInput, Content: "y",
		}, 100, defaultQueueModes())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("first = %d, second = %d", first, second)
	}
	// Another type for the same request id is an error.
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := AdmitSubmission(tx, RootConversationID, SubmissionDraft{
			RequestID: &request, Type: SubmissionTypeWrite, Entry: &EntryDraft{Kind: "note"},
		}, 100, defaultQueueModes())
		return err
	}); err == nil {
		t.Fatal("a request id of another type must fail")
	}
}

func TestAdmitIdleWrite(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	var id Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		id, err = AdmitSubmission(tx, RootConversationID, SubmissionDraft{
			Type: SubmissionTypeWrite, Entry: &EntryDraft{Kind: "note"},
		}, 100, defaultQueueModes())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	record, _ := storage.Submission(ctx, id)
	if record.Status != SubmissionDone || record.Type != SubmissionTypeWrite || record.Entry == nil {
		t.Fatalf("record = %+v", record)
	}
	live, _, _ := session.Snapshot(ctx, LiveDoc.Definition, RootConversationID)
	if _, present := live.(map[string]any)["run"]; present {
		t.Fatalf("a write must not start a run: %+v", live)
	}
}

func TestSubmissionsWaitAndAbort(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	submissions := newTestSubmissions(session, storage)
	// An idle input is placed; its settlement resolves the waiter.
	handle, err := submissions.Submit(RootConversationID, SubmissionDraft{Type: SubmissionTypeInput, Content: "x"}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if record, _ := submissions.Status(handle.ID(), ctx); record.Status != SubmissionPlaced {
		t.Fatalf("status = %+v", record)
	}
	waited := make(chan SubmissionRecord, 1)
	waitErrors := make(chan error, 1)
	go func() {
		record, err := submissions.Wait(handle.ID(), ctx)
		if err != nil {
			waitErrors <- err
			return
		}
		waited <- record
	}()
	answer := Id(500)
	if err := session.Commit(ctx, func(tx *Transaction) error {
		return tx.SettleSubmission(handle.ID(), SubmissionSettlement{Status: SubmissionDone, Answer: &answer})
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case record := <-waited:
		if record.Status != SubmissionDone || record.Answer == nil || *record.Answer != answer {
			t.Fatalf("record = %+v", record)
		}
	case err := <-waitErrors:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("wait did not resolve")
	}
	// An abort of a queued submission settles it unanswered and empties the inbox.
	if err := session.Commit(ctx, func(tx *Transaction) error {
		stageLive(t, tx, RootConversationID, "run", map[string]any{"taskId": 99.0, "inputs": []any{}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	queued, err := submissions.Submit(RootConversationID, SubmissionDraft{Type: SubmissionTypeInput, Content: "queued"}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	result, err := submissions.Abort(queued.ID(), ctx, nil)
	if err != nil || result != "aborted" {
		t.Fatalf("abort = %q, %v", result, err)
	}
	record, _ := storage.Submission(ctx, queued.ID())
	if record.Status != SubmissionUnanswered || record.Reason == nil || *record.Reason != "aborted" {
		t.Fatalf("record = %+v", record)
	}
	if len(readInbox(t, session, RootConversationID).Items) != 0 {
		t.Fatal("the inbox item must be removed")
	}
}

func TestSubmissionsWaitRejectsOnClose(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	submissions := newTestSubmissions(session, storage)
	if err := session.Commit(ctx, func(tx *Transaction) error {
		stageLive(t, tx, RootConversationID, "run", map[string]any{"taskId": 99.0, "inputs": []any{}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	handle, err := submissions.Submit(RootConversationID, SubmissionDraft{Type: SubmissionTypeInput, Content: "x"}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	waitErrors := make(chan error, 1)
	go func() {
		_, err := submissions.Wait(handle.ID(), ctx)
		waitErrors <- err
	}()
	// Wait for the waiter to register, then close.
	deadline := time.Now().Add(2 * time.Second)
	for len(submissions.waiters.Keys()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-waitErrors:
		if err == nil || err.Error() != "Harness is closed" {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("close did not reject the waiter")
	}
}
