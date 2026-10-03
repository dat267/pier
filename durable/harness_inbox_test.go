package durable

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

// Port of harness/inbox.ts.

func stageInbox(t *testing.T, tx *Transaction, conversationID Id, items []InboxItem) {
	t.Helper()
	inbox, err := tx.Doc(InboxDoc.Definition, conversationID)
	if err != nil {
		t.Fatal(err)
	}
	AssignJSON(inbox.(map[string]any), "items", inboxItemsValue(items))
}

func writeItem(t *testing.T, id Id, draft EntryDraft) InboxItem {
	t.Helper()
	encoded, err := marshalJSONValue(draft)
	if err != nil {
		t.Fatal(err)
	}
	return InboxItem{ID: id, Mode: InboxWrite, Entry: json.RawMessage(encoded)}
}

func readInbox(t *testing.T, session *Session, conversationID Id) InboxState {
	t.Helper()
	value, ok, err := session.Snapshot(context.Background(), InboxDoc.Definition, conversationID)
	if err != nil || !ok {
		t.Fatalf("snapshot = %v, %v", ok, err)
	}
	state, err := decodeInboxState(value)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestInboxBoundaryPlacesSelectedItems(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var steerID, followID, writeID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		steer, err := tx.CreateSubmission(SubmissionCreate{ConversationID: RootConversationID, Type: SubmissionTypeInput, Status: SubmissionQueued})
		if err != nil {
			return err
		}
		steerID = steer.ID
		follow, err := tx.CreateSubmission(SubmissionCreate{ConversationID: RootConversationID, Type: SubmissionTypeInput, Status: SubmissionQueued})
		if err != nil {
			return err
		}
		followID = follow.ID
		write, err := tx.CreateSubmission(SubmissionCreate{ConversationID: RootConversationID, Type: SubmissionTypeWrite, Status: SubmissionQueued})
		if err != nil {
			return err
		}
		writeID = write.ID
		stageInbox(t, tx, RootConversationID, []InboxItem{
			{ID: steerID, Mode: InboxSteer, Content: "steer"},
			{ID: followID, Mode: InboxFollowUp, Content: "follow"},
			writeItem(t, writeID, EntryDraft{Kind: "message", Model: []ai.Message{userMessage("written", 1)}}),
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// postTools with one-at-a-time steering: the write and the first steer are
	// placed; the follow-up stays for the final boundary.
	var result BoundaryResult
	if err := session.Commit(ctx, func(tx *Transaction) error {
		boundary, err := PrepareBoundary(tx, RootConversationID, QueueModes{SteeringMode: QueueOneAtATime, FollowUpMode: QueueAll})
		if err != nil {
			return err
		}
		result, err = ApplyBoundary(tx, boundary, "postTools", 100)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(result.Users) != 1 || result.Users[0] != steerID || result.Reset {
		t.Fatalf("result = %+v", result)
	}
	steer, _ := storage.Submission(ctx, steerID)
	if steer.Status != SubmissionPlaced {
		t.Fatalf("steer = %+v", steer)
	}
	write, _ := storage.Submission(ctx, writeID)
	if write.Status != SubmissionDone {
		t.Fatalf("write = %+v", write)
	}
	follow, _ := storage.Submission(ctx, followID)
	if follow.Status != SubmissionQueued {
		t.Fatalf("follow = %+v", follow)
	}
	if items := readInbox(t, session, RootConversationID).Items; len(items) != 1 || items[0].ID != followID {
		t.Fatalf("items = %+v", items)
	}

	// The final boundary places the follow-up and empties the queue.
	if err := session.Commit(ctx, func(tx *Transaction) error {
		boundary, err := PrepareBoundary(tx, RootConversationID, QueueModes{SteeringMode: QueueOneAtATime, FollowUpMode: QueueAll})
		if err != nil {
			return err
		}
		result, err = ApplyBoundary(tx, boundary, "final", 200)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(result.Users) != 1 || result.Users[0] != followID {
		t.Fatalf("result = %+v", result)
	}
	if items := readInbox(t, session, RootConversationID).Items; len(items) != 0 {
		t.Fatalf("items = %+v", items)
	}
}

func TestInboxResetTurnsBoundaryFinal(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var followID, resetID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		follow, err := tx.CreateSubmission(SubmissionCreate{ConversationID: RootConversationID, Type: SubmissionTypeInput, Status: SubmissionQueued})
		if err != nil {
			return err
		}
		followID = follow.ID
		reset, err := tx.CreateSubmission(SubmissionCreate{ConversationID: RootConversationID, Type: SubmissionTypeWrite, Status: SubmissionQueued})
		if err != nil {
			return err
		}
		resetID = reset.ID
		stageInbox(t, tx, RootConversationID, []InboxItem{
			{ID: followID, Mode: InboxFollowUp, Content: "follow"},
			writeItem(t, resetID, EntryDraft{Kind: "pi.reset", HeadSelf: true}),
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var result BoundaryResult
	if err := session.Commit(ctx, func(tx *Transaction) error {
		boundary, err := PrepareBoundary(tx, RootConversationID, QueueModes{SteeringMode: QueueAll, FollowUpMode: QueueAll})
		if err != nil {
			return err
		}
		result, err = ApplyBoundary(tx, boundary, "postTools", 100)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// The reset write made the postTools boundary final, so the follow-up ran.
	if !result.Reset || len(result.Users) != 1 || result.Users[0] != followID {
		t.Fatalf("result = %+v", result)
	}
}

func TestInboxStaleWriteSettlesUnanswered(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// An older entry than the boundary head.
	mustCommit(t, storage, contextEntry(50, RootConversationID, nil, nil, nil))
	head := Id(50)
	mustCommit(t, storage, contextEntry(60, RootConversationID, &head, nil, nil))
	var writeID Id
	staleHead := Id(10)
	if err := session.Commit(ctx, func(tx *Transaction) error {
		write, err := tx.CreateSubmission(SubmissionCreate{ConversationID: RootConversationID, Type: SubmissionTypeWrite, Status: SubmissionQueued})
		if err != nil {
			return err
		}
		writeID = write.ID
		draft := EntryDraft{Kind: "message", Head: &staleHead}
		stageInbox(t, tx, RootConversationID, []InboxItem{writeItem(t, writeID, draft)})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(ctx, func(tx *Transaction) error {
		boundary, err := PrepareBoundary(tx, RootConversationID, QueueModes{SteeringMode: QueueAll, FollowUpMode: QueueAll})
		if err != nil {
			return err
		}
		_, err = ApplyBoundary(tx, boundary, "final", 100)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	record, _ := storage.Submission(ctx, writeID)
	if record.Status != SubmissionUnanswered || record.Reason == nil || *record.Reason != "stale" {
		t.Fatalf("record = %+v", record)
	}
}

func TestWithdrawQueuedInputsKeepsWrites(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var inputID, writeID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		input, err := tx.CreateSubmission(SubmissionCreate{ConversationID: RootConversationID, Type: SubmissionTypeInput, Status: SubmissionQueued})
		if err != nil {
			return err
		}
		inputID = input.ID
		write, err := tx.CreateSubmission(SubmissionCreate{ConversationID: RootConversationID, Type: SubmissionTypeWrite, Status: SubmissionQueued})
		if err != nil {
			return err
		}
		writeID = write.ID
		stageInbox(t, tx, RootConversationID, []InboxItem{
			{ID: inputID, Mode: InboxSteer, Content: "x"},
			writeItem(t, writeID, EntryDraft{Kind: "message"}),
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(ctx, func(tx *Transaction) error {
		return WithdrawQueuedInputs(tx, RootConversationID)
	}); err != nil {
		t.Fatal(err)
	}
	input, _ := storage.Submission(ctx, inputID)
	if input.Status != SubmissionUnanswered || input.Reason == nil || *input.Reason != "aborted" {
		t.Fatalf("input = %+v", input)
	}
	// The queued write is still in the inbox for a later boundary.
	items := readInbox(t, session, RootConversationID).Items
	if len(items) != 1 || items[0].ID != writeID {
		t.Fatalf("items = %+v", items)
	}
}
