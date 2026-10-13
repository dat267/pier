package durable

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

func TestAdmitInputCanUseAnApplicationEntry(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	inputEntry := &EntryDraft{Kind: "chat.mention", Data: json.RawMessage(`{"author":"ann"}`)}
	var id Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		id, err = AdmitSubmission(tx, RootConversationID, SubmissionDraft{
			Type: SubmissionTypeInput, Content: "ann: hello", Entry: inputEntry,
		}, 100, defaultQueueModes())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	record, err := storage.Submission(ctx, id)
	if err != nil || record == nil || record.Entry == nil {
		t.Fatalf("input submission = %+v, err=%v", record, err)
	}
	commit, err := storage.Entry(ctx, *record.Entry)
	if err != nil || commit == nil || commit.Entry.Kind != inputEntry.Kind || string(commit.Entry.Data) != string(inputEntry.Data) {
		t.Fatalf("placed input entry = %+v, err=%v", commit, err)
	}
	message, ok := commit.Entry.Model[0].(*ai.UserMessage)
	if !ok || message.Content.Text != "ann: hello" {
		t.Fatalf("input message = %+v", commit.Entry.Model)
	}
}

func TestInputRejectsSystemKindsAsApplicationEntries(t *testing.T) {
	for _, kind := range []string{"", "pi.user", "pi.compaction"} {
		t.Run(kind, func(t *testing.T) {
			session, _ := newRootSession(t)
			err := session.Commit(context.Background(), func(tx *Transaction) error {
				_, err := AdmitSubmission(tx, RootConversationID, SubmissionDraft{
					Type: SubmissionTypeInput, Content: "hello", Entry: &EntryDraft{Kind: kind},
				}, 100, defaultQueueModes())
				return err
			})
			if err == nil || !strings.Contains(err.Error(), "input entry's kind") {
				t.Fatalf("invalid input kind %q error = %v", kind, err)
			}
		})
	}
}

func TestBusyInputQueuesItsApplicationEntry(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		stageLive(t, tx, RootConversationID, "run", map[string]any{"taskId": 99.0, "inputs": []any{}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	entry := &EntryDraft{Kind: "chat.mention", Data: json.RawMessage(`{"author":"ann"}`)}
	var submissionID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		submissionID, err = AdmitSubmission(tx, RootConversationID, SubmissionDraft{
			Type: SubmissionTypeInput, Content: "ann: hello", Entry: entry,
		}, 100, defaultQueueModes())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	items := readInbox(t, session, RootConversationID).Items
	if len(items) != 1 || items[0].Mode != InboxFollowUp {
		t.Fatalf("inbox = %+v", items)
	}
	queuedEntry, err := decodeInboxEntry(items[0])
	if err != nil || queuedEntry.Kind != entry.Kind || string(queuedEntry.Data) != string(entry.Data) {
		t.Fatalf("queued input entry = %+v, err=%v", queuedEntry, err)
	}
	if err := session.Commit(ctx, func(tx *Transaction) error {
		live, err := tx.Doc(LiveDoc.Definition, RootConversationID)
		if err != nil {
			return err
		}
		AssignJSON(live.(map[string]any), "run", nil)
		boundary, err := PrepareBoundary(tx, RootConversationID, defaultQueueModes())
		if err != nil {
			return err
		}
		_, err = ApplyBoundary(tx, boundary, "final", 101)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	record, err := storage.Submission(ctx, submissionID)
	if err != nil || record == nil || record.Entry == nil {
		t.Fatalf("placed input submission = %+v, err=%v", record, err)
	}
	placed, err := storage.Entry(ctx, *record.Entry)
	if err != nil || placed == nil || placed.Entry.Kind != entry.Kind || string(placed.Entry.Data) != string(entry.Data) {
		t.Fatalf("placed application entry = %+v, err=%v", placed, err)
	}
	message, ok := placed.Entry.Model[0].(*ai.UserMessage)
	if !ok || message.Content.Text != "ann: hello" {
		t.Fatalf("placed user content = %+v", placed.Entry.Model)
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

func TestDisplayOnlyWriteSettlesImmediatelyWhileOtherInputsAreQueued(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		stageLive(t, tx, RootConversationID, "run", map[string]any{"taskId": 99.0, "inputs": []any{}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	queuedID, err := func() (Id, error) {
		var id Id
		err := session.Commit(ctx, func(tx *Transaction) error {
			var admitErr error
			id, admitErr = AdmitSubmission(tx, RootConversationID, SubmissionDraft{Type: SubmissionTypeInput, Content: "queued input"}, 100, defaultQueueModes())
			return admitErr
		})
		return id, err
	}()
	if err != nil {
		t.Fatal(err)
	}

	entryDraft := EntryDraft{Kind: "notice", Data: json.RawMessage(`{"text":"visible"}`)}
	var writeID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var admitErr error
		writeID, admitErr = AdmitSubmission(tx, RootConversationID, SubmissionDraft{Type: SubmissionTypeWrite, Entry: &entryDraft}, 101, defaultQueueModes())
		return admitErr
	}); err != nil {
		t.Fatal(err)
	}
	write, err := storage.Submission(ctx, writeID)
	if err != nil || write == nil || write.Status != SubmissionDone || write.Entry == nil {
		t.Fatalf("display-only write submission = %+v, err=%v", write, err)
	}
	storedEntry, err := storage.Entry(ctx, *write.Entry)
	if err != nil || storedEntry == nil || storedEntry.Entry.Kind != "notice" || string(storedEntry.Entry.Data) != string(entryDraft.Data) {
		t.Fatalf("display-only entry = %+v, err=%v", storedEntry, err)
	}
	inbox := readInbox(t, session, RootConversationID)
	if len(inbox.Items) != 1 || inbox.Items[0].ID != queuedID {
		t.Fatalf("display-only write entered the inbox: %+v", inbox.Items)
	}
}

func TestDisplayOnlyWriteWaitsBehindQueuedReset(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		stageLive(t, tx, RootConversationID, "run", map[string]any{"taskId": 99.0, "inputs": []any{}})
		_, err := AdmitSubmission(tx, RootConversationID, SubmissionDraft{
			Type: SubmissionTypeWrite, Entry: &EntryDraft{Kind: "pi.reset", HeadSelf: true},
		}, 100, defaultQueueModes())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	entry := EntryDraft{Kind: "notice", Data: json.RawMessage(`{"text":"after reset"}`)}
	var id Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		id, err = AdmitSubmission(tx, RootConversationID, SubmissionDraft{Type: SubmissionTypeWrite, Entry: &entry}, 101, defaultQueueModes())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	record, err := storage.Submission(ctx, id)
	if err != nil || record == nil || record.Status != SubmissionQueued {
		t.Fatalf("display-only write after reset = %+v, err=%v; want queued", record, err)
	}
	items := readInbox(t, session, RootConversationID).Items
	if len(items) != 2 || items[1].ID != id || items[1].Mode != InboxWrite {
		t.Fatalf("inbox = %+v, want reset followed by display-only write", items)
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
	live, exists, _ := session.Snapshot(ctx, LiveDoc.Definition, RootConversationID)
	if exists {
		if _, present := live.(map[string]any)["run"]; present {
			t.Fatalf("a write must not start a run: %+v", live)
		}
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
