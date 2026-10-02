package durable

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Port of cases from upstream
// packages/durable/src/testing/storage-conformance.ts, run against every
// backend. Test names follow the upstream case names.

// conformance runs one case against memory, SQLite and JSONL.
func conformance(t *testing.T, run func(t *testing.T, storage Storage)) {
	t.Helper()
	t.Run("memory", func(t *testing.T) { run(t, NewMemoryStorage()) })
	t.Run("sqlite", func(t *testing.T) { run(t, newSqliteStorageConformance(t)) })
	t.Run("jsonl", func(t *testing.T) { run(t, newJsonlStorageConformance(t)) })
}

func testEntry(id, conversationID Id, kind string, head *Id) *EntryRecord {
	return &EntryRecord{ID: id, ConversationID: conversationID, Kind: kind, Head: head}
}

// TestConformanceIndexesEntriesCommittedOutOfIDOrder covers upstream's
// "indexes entries committed out of ID order".
func TestConformanceIndexesEntriesCommittedOutOfIDOrder(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		mustCommit(t, storage, conversationWrite(RootConversationID))
		head := Id(10)
		mustCommit(t, storage,
			StorageWrite{Type: "entry", Entry: testEntry(30, RootConversationID, "message", nil)},
			StorageWrite{Type: "entry", Entry: testEntry(10, RootConversationID, "message", nil)},
			StorageWrite{Type: "entry", Entry: testEntry(20, RootConversationID, "marker", &head)},
		)
		page, err := storage.ScanEntries(ctx, EntryQuery{ConversationID: RootConversationID}, nil, 10)
		if err != nil {
			t.Fatal(err)
		}
		var ids []Id
		for _, entry := range page.Items {
			ids = append(ids, entry.ID)
		}
		if len(ids) != 3 || ids[0] != 30 || ids[1] != 20 || ids[2] != 10 {
			t.Fatalf("ids = %v", ids)
		}
		marker, err := storage.FindLatestHeadMarker(ctx, RootConversationID, nil)
		if err != nil || marker == nil || marker.ID != 20 {
			t.Fatalf("marker = %+v, %v", marker, err)
		}
	})
}

// TestConformanceEntryCursorBelowLastItemAfterNewerCommit covers upstream's
// "continues an entry cursor below its last item after a newer commit".
func TestConformanceEntryCursorBelowLastItemAfterNewerCommit(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		mustCommit(t, storage, conversationWrite(RootConversationID))
		oldest := mustMintID(t, storage)
		middle := mustMintID(t, storage)
		newest := mustMintID(t, storage)
		mustCommit(t, storage,
			StorageWrite{Type: "entry", Entry: testEntry(oldest, RootConversationID, "message", nil)},
			StorageWrite{Type: "entry", Entry: testEntry(middle, RootConversationID, "message", nil)},
			StorageWrite{Type: "entry", Entry: testEntry(newest, RootConversationID, "message", nil)},
		)
		first, err := storage.ScanEntries(ctx, EntryQuery{ConversationID: RootConversationID}, nil, 2)
		if err != nil || len(first.Items) != 2 || first.Items[0].ID != newest || first.Items[1].ID != middle {
			t.Fatalf("first = %+v, %v", first.Items, err)
		}
		appended := mustMintID(t, storage)
		mustCommit(t, storage, StorageWrite{Type: "entry", Entry: testEntry(appended, RootConversationID, "message", nil)})
		second, err := storage.ScanEntries(ctx, EntryQuery{ConversationID: RootConversationID}, first.Next, 2)
		if err != nil || len(second.Items) != 1 || second.Items[0].ID != oldest || second.Next != nil {
			t.Fatalf("second = %+v, %v", second.Items, err)
		}
	})
}

// TestConformanceFiltersAndPagesConversationsByOwnerEdges covers upstream's
// "filters and pages conversations by durable owner edges".
func TestConformanceFiltersAndPagesConversationsByOwnerEdges(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		mustCommit(t, storage, conversationWrite(RootConversationID))
		otherOwner := mustMintID(t, storage)
		firstTask := mustMintID(t, storage)
		secondTask := mustMintID(t, storage)
		first := mustMintID(t, storage)
		second := mustMintID(t, storage)
		third := mustMintID(t, storage)
		mustCommit(t, storage,
			conversationWrite(otherOwner),
			StorageWrite{Type: "conversation", Conversation: &ConversationRecord{ID: first,
				Owner: &ConversationOwner{ConversationID: RootConversationID, TaskID: firstTask}}},
			StorageWrite{Type: "conversation", Conversation: &ConversationRecord{ID: second,
				Owner: &ConversationOwner{ConversationID: RootConversationID, TaskID: secondTask}}},
			StorageWrite{Type: "conversation", Conversation: &ConversationRecord{ID: third,
				Owner: &ConversationOwner{ConversationID: otherOwner, TaskID: firstTask}}},
		)
		ownerConversation := RootConversationID
		page, err := storage.ScanConversations(ctx, ConversationQuery{OwnerConversationID: &ownerConversation}, nil, 1)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != first || page.Next == nil {
			t.Fatalf("first page = %+v, %v", page.Items, err)
		}
		page, err = storage.ScanConversations(ctx, ConversationQuery{OwnerConversationID: &ownerConversation}, page.Next, 1)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != second || page.Next != nil {
			t.Fatalf("second page = %+v, %v", page.Items, err)
		}
		ownerTask := firstTask
		page, err = storage.ScanConversations(ctx, ConversationQuery{OwnerTaskID: &ownerTask}, nil, 10)
		if err != nil || len(page.Items) != 2 || page.Items[0].ID != first || page.Items[1].ID != third {
			t.Fatalf("owner task = %+v, %v", page.Items, err)
		}
		page, err = storage.ScanConversations(ctx, ConversationQuery{OwnerConversationID: &ownerConversation, OwnerTaskID: &ownerTask}, nil, 10)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != first {
			t.Fatalf("both = %+v, %v", page.Items, err)
		}
	})
}

func pendingTaskRecord(id, conversationID Id) *TaskRecord {
	return &TaskRecord{
		ID: id, ConversationID: conversationID, Kind: "t", Version: 1, Input: json.RawMessage(`{}`),
		State: TaskState{Status: TaskPending, Checkpoint: json.RawMessage(`{"phase":"start"}`)},
	}
}

// TestConformanceReplacesTaskRecordsAndPagesFilteredScans covers upstream's
// "replaces complete task records and pages filtered task scans".
func TestConformanceReplacesTaskRecordsAndPagesFilteredScans(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		mustCommit(t, storage, conversationWrite(RootConversationID))
		first := mustMintID(t, storage)
		second := mustMintID(t, storage)
		third := mustMintID(t, storage)
		firstTask := pendingTaskRecord(first, RootConversationID)
		firstTask.Memos = map[string]json.RawMessage{"winner": json.RawMessage(`"first"`)}
		secondTask := pendingTaskRecord(second, RootConversationID)
		secondTask.Background = true
		thirdTask := pendingTaskRecord(third, RootConversationID)
		thirdTask.AbortRequested = true
		mustCommit(t, storage,
			StorageWrite{Type: "task", Task: firstTask},
			StorageWrite{Type: "task", Task: secondTask},
			StorageWrite{Type: "task", Task: thirdTask},
		)

		// A complete replacement changes state, memos and abort mark.
		running := *firstTask
		running.State = TaskState{Status: TaskRunning, Checkpoint: json.RawMessage(`{"phase":"effect"}`)}
		running.AbortRequested = true
		running.Memos = nil
		mustCommit(t, storage, StorageWrite{Type: "task", Task: &running})
		stored, err := storage.Task(ctx, first)
		if err != nil || stored.State.Status != TaskRunning || !stored.AbortRequested || stored.Memos != nil {
			t.Fatalf("running = %+v, %v", stored, err)
		}
		terminal := *firstTask
		terminal.State = TaskState{Status: TaskTerminal, Outcome: &TaskOutcome{Status: OutcomeCompleted, Result: json.RawMessage(`{"entryId":99}`)}}
		terminal.AbortRequested = true
		terminal.Memos = nil
		mustCommit(t, storage, StorageWrite{Type: "task", Task: &terminal})
		stored, err = storage.Task(ctx, first)
		if err != nil || stored.State.Status != TaskTerminal || stored.State.Outcome == nil {
			t.Fatalf("terminal = %+v, %v", stored, err)
		}

		pending := TaskPending
		page, err := storage.ScanTasks(ctx, TaskQuery{Status: &pending}, nil, 1)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != second || page.Next == nil {
			t.Fatalf("pending page = %+v, %v", page.Items, err)
		}
		page, err = storage.ScanTasks(ctx, TaskQuery{Status: &pending}, page.Next, 1)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != third {
			t.Fatalf("pending second = %+v, %v", page.Items, err)
		}
		status := TaskTerminal
		aborted := true
		page, err = storage.ScanTasks(ctx, TaskQuery{Status: &status, AbortRequested: &aborted}, nil, 10)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != first {
			t.Fatalf("terminal abort = %+v, %v", page.Items, err)
		}
		background := true
		page, err = storage.ScanTasks(ctx, TaskQuery{Background: &background}, nil, 10)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != second {
			t.Fatalf("background = %+v, %v", page.Items, err)
		}
	})
}

// TestConformanceStoresOwnersAndScansWaitingAndCompleting covers upstream's
// "stores owners and scans waiting and completing tasks by status".
func TestConformanceStoresOwnersAndScansWaitingAndCompleting(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		mustCommit(t, storage, conversationWrite(RootConversationID))
		ownerID := mustMintID(t, storage)
		waitingID := mustMintID(t, storage)
		completingID := mustMintID(t, storage)
		owner := pendingTaskRecord(ownerID, RootConversationID)
		waiting := pendingTaskRecord(waitingID, RootConversationID)
		ownerRef := ownerID
		waiting.Owner = &ownerRef
		waiting.State = TaskState{Status: TaskWaiting, Checkpoint: json.RawMessage(`{"phase":"next"}`), On: []Id{ownerID}, Policy: JoinAllSettled}
		waiting.Memos = map[string]json.RawMessage{"kept": json.RawMessage(`true`)}
		completing := pendingTaskRecord(completingID, RootConversationID)
		completing.Owner = &ownerRef
		completing.State = TaskState{Status: TaskCompleting, Outcome: &TaskOutcome{Status: OutcomeFailed, Error: &StoredError{Message: "held"}}}
		completing.Memos = nil
		mustCommit(t, storage,
			StorageWrite{Type: "task", Task: owner},
			StorageWrite{Type: "task", Task: waiting},
			StorageWrite{Type: "task", Task: completing},
		)
		stored, err := storage.Task(ctx, waitingID)
		if err != nil || stored.State.Status != TaskWaiting || stored.Owner == nil || *stored.Owner != ownerID ||
			len(stored.State.On) != 1 || stored.State.Policy != JoinAllSettled {
			t.Fatalf("waiting = %+v, %v", stored, err)
		}
		stored, err = storage.Task(ctx, completingID)
		if err != nil || stored.State.Status != TaskCompleting || stored.State.Outcome == nil {
			t.Fatalf("completing = %+v, %v", stored, err)
		}
		status := TaskWaiting
		page, err := storage.ScanTasks(ctx, TaskQuery{Status: &status}, nil, 10)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != waitingID {
			t.Fatalf("waiting scan = %+v, %v", page.Items, err)
		}
		status = TaskCompleting
		page, err = storage.ScanTasks(ctx, TaskQuery{Status: &status}, nil, 10)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != completingID {
			t.Fatalf("completing scan = %+v, %v", page.Items, err)
		}
		status = TaskPending
		page, err = storage.ScanTasks(ctx, TaskQuery{Status: &status}, nil, 10)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != ownerID {
			t.Fatalf("pending scan = %+v, %v", page.Items, err)
		}
		// A completing task becomes terminal and moves between status scans.
		terminal := *completing
		terminal.State = TaskState{Status: TaskTerminal, Outcome: completing.State.Outcome}
		mustCommit(t, storage, StorageWrite{Type: "task", Task: &terminal})
		status = TaskCompleting
		page, _ = storage.ScanTasks(ctx, TaskQuery{Status: &status}, nil, 10)
		if len(page.Items) != 0 {
			t.Fatalf("completing after terminal = %+v", page.Items)
		}
		status = TaskTerminal
		page, _ = storage.ScanTasks(ctx, TaskQuery{Status: &status}, nil, 10)
		if len(page.Items) != 1 || page.Items[0].ID != completingID {
			t.Fatalf("terminal scan = %+v", page.Items)
		}
	})
}

// TestConformanceIndexesRequestIDsAndReplacesSubmissions covers upstream's
// "indexes request IDs per conversation and replaces complete submission
// records".
func TestConformanceIndexesRequestIDsAndReplacesSubmissions(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		mustCommit(t, storage, conversationWrite(RootConversationID))
		secondConversation := mustMintID(t, storage)
		mustCommit(t, storage, conversationWrite(secondConversation))
		firstID := mustMintID(t, storage)
		secondID := mustMintID(t, storage)
		otherID := mustMintID(t, storage)
		same := "same"
		other := "other"
		first := &SubmissionRecord{ID: firstID, ConversationID: RootConversationID, RequestID: &same, Type: SubmissionTypeInput, Status: SubmissionQueued}
		second := &SubmissionRecord{ID: secondID, ConversationID: RootConversationID, RequestID: &other, Type: SubmissionTypeInput, Status: SubmissionQueued}
		otherConversation := &SubmissionRecord{ID: otherID, ConversationID: secondConversation, RequestID: &same, Type: SubmissionTypeInput, Status: SubmissionQueued}
		mustCommit(t, storage,
			StorageWrite{Type: "submission", Submission: first},
			StorageWrite{Type: "submission", Submission: second},
			StorageWrite{Type: "submission", Submission: otherConversation},
		)
		found, err := storage.SubmissionByRequest(ctx, RootConversationID, "same")
		if err != nil || found == nil || found.ID != firstID {
			t.Fatalf("by request = %+v, %v", found, err)
		}
		found, err = storage.SubmissionByRequest(ctx, secondConversation, "same")
		if err != nil || found == nil || found.ID != otherID {
			t.Fatalf("other conversation = %+v, %v", found, err)
		}

		entry := mustMintID(t, storage)
		placedSecond := *second
		placedSecond.Status = SubmissionPlaced
		placedSecond.Entry = &entry
		mustCommit(t, storage, StorageWrite{Type: "submission", Submission: &placedSecond})
		found, _ = storage.Submission(ctx, secondID)
		if found.Status != SubmissionPlaced || found.Entry == nil || *found.Entry != entry {
			t.Fatalf("placed = %+v", found)
		}
		found, _ = storage.SubmissionByRequest(ctx, RootConversationID, "other")
		if found.Status != SubmissionPlaced {
			t.Fatalf("placed by request = %+v", found)
		}

		ids := func(query SubmissionQuery) []Id {
			var found []Id
			var cursor Cursor
			for {
				page, err := storage.ScanSubmissions(ctx, query, cursor, 1)
				if err != nil {
					t.Fatal(err)
				}
				for _, item := range page.Items {
					found = append(found, item.ID)
				}
				cursor = page.Next
				if cursor == nil {
					return found
				}
			}
		}
		if got := ids(SubmissionQuery{}); len(got) != 3 || got[0] != firstID || got[1] != secondID || got[2] != otherID {
			t.Fatalf("all = %v", got)
		}
		conversation := RootConversationID
		if got := ids(SubmissionQuery{ConversationID: &conversation}); len(got) != 2 || got[0] != firstID || got[1] != secondID {
			t.Fatalf("conversation = %v", got)
		}
		queued := SubmissionQueued
		if got := ids(SubmissionQuery{Status: &queued}); len(got) != 2 || got[0] != firstID || got[1] != otherID {
			t.Fatalf("queued = %v", got)
		}
		placed := SubmissionPlaced
		if got := ids(SubmissionQuery{Status: &placed}); len(got) != 1 || got[0] != secondID {
			t.Fatalf("placed = %v", got)
		}
		if got := ids(SubmissionQuery{ConversationID: &secondConversation, Status: &queued}); len(got) != 1 || got[0] != otherID {
			t.Fatalf("other conversation queued = %v", got)
		}
		if got := ids(SubmissionQuery{ConversationID: &secondConversation, Status: &placed}); len(got) != 0 {
			t.Fatalf("other conversation placed = %v", got)
		}
		page, err := storage.ScanSubmissions(ctx, SubmissionQuery{Status: &placed}, nil, 10)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != secondID {
			t.Fatalf("placed scan = %+v, %v", page.Items, err)
		}
	})
}

// TestConformanceStoresPassiveWriteSubmissions covers upstream's "stores
// passive write submissions without input-only lifecycle states".
func TestConformanceStoresPassiveWriteSubmissions(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		mustCommit(t, storage, conversationWrite(RootConversationID))
		doneID := mustMintID(t, storage)
		failedID := mustMintID(t, storage)
		doneRequest := "passive-done"
		failedRequest := "passive-failed"
		mustCommit(t, storage,
			StorageWrite{Type: "submission", Submission: &SubmissionRecord{ID: doneID, ConversationID: RootConversationID,
				RequestID: &doneRequest, Type: SubmissionTypeWrite, Status: SubmissionQueued}},
			StorageWrite{Type: "submission", Submission: &SubmissionRecord{ID: failedID, ConversationID: RootConversationID,
				RequestID: &failedRequest, Type: SubmissionTypeWrite, Status: SubmissionQueued}},
		)
		entry := mustMintID(t, storage)
		done := &SubmissionRecord{ID: doneID, ConversationID: RootConversationID, RequestID: &doneRequest,
			Type: SubmissionTypeWrite, Status: SubmissionDone, Entry: &entry}
		reason := "closed"
		unanswered := &SubmissionRecord{ID: failedID, ConversationID: RootConversationID, RequestID: &failedRequest,
			Type: SubmissionTypeWrite, Status: SubmissionUnanswered, Reason: &reason, Detail: json.RawMessage(`{"retryable":false}`)}
		mustCommit(t, storage,
			StorageWrite{Type: "submission", Submission: done},
			StorageWrite{Type: "submission", Submission: unanswered},
		)
		found, err := storage.Submission(ctx, doneID)
		if err != nil || found.Type != SubmissionTypeWrite || found.Status != SubmissionDone || found.Entry == nil {
			t.Fatalf("done = %+v, %v", found, err)
		}
		found, _ = storage.SubmissionByRequest(ctx, RootConversationID, "passive-done")
		if found.Status != SubmissionDone {
			t.Fatalf("done by request = %+v", found)
		}
		found, _ = storage.Submission(ctx, failedID)
		if found.Status != SubmissionUnanswered || found.Reason == nil || *found.Reason != "closed" {
			t.Fatalf("unanswered = %+v", found)
		}
		found, _ = storage.SubmissionByRequest(ctx, RootConversationID, "passive-failed")
		if found.Status != SubmissionUnanswered {
			t.Fatalf("unanswered by request = %+v", found)
		}
	})
}

// TestConformanceIndexesLogicalAddressesAndExactScopeScans covers upstream's
// "indexes logical addresses and exact-scope scans independently".
func TestConformanceIndexesLogicalAddressesAndExactScopeScans(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		mustCommit(t, storage, conversationWrite(RootConversationID))
		firstID := mustMintID(t, storage)
		secondID := mustMintID(t, storage)
		conversationDocID := mustMintID(t, storage)
		rootConversation := RootConversationID
		taskID := mustMintID(t, storage)
		taskSingletonID := mustMintID(t, storage)
		taskFamilyID := mustMintID(t, storage)
		taskOtherKindID := mustMintID(t, storage)
		proto := "__proto__"
		constructor := "constructor"
		history, fork := HistoryLatest, ForkCurrent
		member := "member"
		mustCommit(t, storage,
			StorageWrite{Type: "task", Task: pendingTaskRecord(taskID, RootConversationID)},
			documentCreateWrite(firstID, "cache", &proto, DocumentScope{Kind: ScopeSession}, json.RawMessage(`{"owner":"first"}`)),
			documentCreateWrite(secondID, "cache", &constructor, DocumentScope{Kind: ScopeSession}, json.RawMessage(`{"owner":"second"}`)),
			documentCreateWrite(conversationDocID, "cache", &proto, DocumentScope{Kind: ScopeConversation, ConversationID: &rootConversation}, json.RawMessage(`{"owner":"conversation"}`), &history, &fork),
			documentCreateWrite(taskSingletonID, "task.cache", nil, DocumentScope{Kind: ScopeTask, TaskID: &taskID}, json.RawMessage(`{"owner":"singleton"}`)),
			documentCreateWrite(taskFamilyID, "task.cache", &member, DocumentScope{Kind: ScopeTask, TaskID: &taskID}, json.RawMessage(`{"owner":"family"}`)),
			documentCreateWrite(taskOtherKindID, "task.other", nil, DocumentScope{Kind: ScopeTask, TaskID: &taskID}, json.RawMessage(`{"owner":"other"}`)),
		)
		record, err := storage.FindDocument(ctx, DocumentAddress{Kind: "cache", Scope: DocumentScope{Kind: ScopeSession}, Key: &proto}, CurrentDocumentPoint())
		if err != nil || record == nil || record.ID != firstID {
			t.Fatalf("proto address = %+v, %v", record, err)
		}
		page, err := storage.ScanDocuments(ctx, DocumentQuery{Scope: DocumentScope{Kind: ScopeSession}, At: CurrentDocumentPoint()}, nil, 1)
		if err != nil || len(page.Items) != 1 || page.Next == nil {
			t.Fatalf("session page = %+v, %v", page.Items, err)
		}
		second, err := storage.ScanDocuments(ctx, DocumentQuery{Scope: DocumentScope{Kind: ScopeSession}, At: CurrentDocumentPoint()}, page.Next, 1)
		if err != nil || len(second.Items) != 1 {
			t.Fatalf("session page 2 = %+v, %v", second.Items, err)
		}
		ids := []Id{page.Items[0].ID, second.Items[0].ID}
		if ids[0] != firstID || ids[1] != secondID {
			t.Fatalf("session ids = %v", ids)
		}
		conversationScope := DocumentScope{Kind: ScopeConversation, ConversationID: &rootConversation}
		page, err = storage.ScanDocuments(ctx, DocumentQuery{Scope: conversationScope, At: CurrentDocumentPoint()}, nil, 10)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != conversationDocID {
			t.Fatalf("conversation scope = %+v, %v", page.Items, err)
		}
		taskScope := DocumentScope{Kind: ScopeTask, TaskID: &taskID}
		record, err = storage.FindDocument(ctx, DocumentAddress{Kind: "task.cache", Scope: taskScope}, CurrentDocumentPoint())
		if err != nil || record == nil || record.ID != taskSingletonID {
			t.Fatalf("task singleton = %+v, %v", record, err)
		}
		record, err = storage.FindDocument(ctx, DocumentAddress{Kind: "task.cache", Scope: taskScope, Key: &member}, CurrentDocumentPoint())
		if err != nil || record == nil || record.ID != taskFamilyID {
			t.Fatalf("task family = %+v, %v", record, err)
		}
		kind := "task.cache"
		page, err = storage.ScanDocuments(ctx, DocumentQuery{Scope: taskScope, At: CurrentDocumentPoint(), Kind: &kind}, nil, 10)
		if err != nil || len(page.Items) != 2 || page.Items[0].ID != taskSingletonID || page.Items[1].ID != taskFamilyID {
			t.Fatalf("task scan = %+v, %v", page.Items, err)
		}
		// The task singleton is current-only: a historical read is rejected.
		if _, err := storage.Document(ctx, taskSingletonID, DocumentPointAt(1)); err == nil ||
			!strings.Contains(err.Error(), "does not retain historical content") {
			t.Fatalf("historical = %v", err)
		}
	})
}

// TestConformanceKeepsIndexedStringIdentitiesLossless covers upstream's "keeps
// indexed string identities lossless": a prototype-like key must round-trip
// through every index.
func TestConformanceKeepsIndexedStringIdentitiesLossless(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		mustCommit(t, storage, conversationWrite(RootConversationID))
		proto := "__proto__"
		constructor := "constructor"
		firstTask := mustMintID(t, storage)
		secondTask := mustMintID(t, storage)
		firstSubmission := mustMintID(t, storage)
		secondSubmission := mustMintID(t, storage)
		firstKindDoc := mustMintID(t, storage)
		secondKindDoc := mustMintID(t, storage)
		firstKeyDoc := mustMintID(t, storage)
		secondKeyDoc := mustMintID(t, storage)
		firstRecord := pendingTaskRecord(firstTask, RootConversationID)
		firstRecord.Kind = proto
		secondRecord := pendingTaskRecord(secondTask, RootConversationID)
		secondRecord.Kind = constructor
		mustCommit(t, storage,
			StorageWrite{Type: "task", Task: firstRecord},
			StorageWrite{Type: "task", Task: secondRecord},
			StorageWrite{Type: "submission", Submission: &SubmissionRecord{ID: firstSubmission, ConversationID: RootConversationID,
				RequestID: &proto, Type: SubmissionTypeInput, Status: SubmissionQueued}},
			StorageWrite{Type: "submission", Submission: &SubmissionRecord{ID: secondSubmission, ConversationID: RootConversationID,
				RequestID: &constructor, Type: SubmissionTypeInput, Status: SubmissionQueued}},
			documentCreateWrite(firstKindDoc, proto, nil, DocumentScope{Kind: ScopeSession}, json.RawMessage(`{"identity":"first kind"}`)),
			documentCreateWrite(secondKindDoc, constructor, nil, DocumentScope{Kind: ScopeSession}, json.RawMessage(`{"identity":"second kind"}`)),
			documentCreateWrite(firstKeyDoc, "family", &proto, DocumentScope{Kind: ScopeSession}, json.RawMessage(`{"identity":"first key"}`)),
			documentCreateWrite(secondKeyDoc, "family", &constructor, DocumentScope{Kind: ScopeSession}, json.RawMessage(`{"identity":"second key"}`)),
		)
		page, err := storage.ScanTasks(ctx, TaskQuery{Kind: &proto}, nil, 10)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != firstTask {
			t.Fatalf("proto task = %+v, %v", page.Items, err)
		}
		page, err = storage.ScanTasks(ctx, TaskQuery{Kind: &constructor}, nil, 10)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != secondTask {
			t.Fatalf("constructor task = %+v, %v", page.Items, err)
		}
		found, _ := storage.SubmissionByRequest(ctx, RootConversationID, proto)
		if found == nil || found.ID != firstSubmission {
			t.Fatalf("proto submission = %+v", found)
		}
		found, _ = storage.SubmissionByRequest(ctx, RootConversationID, constructor)
		if found == nil || found.ID != secondSubmission {
			t.Fatalf("constructor submission = %+v", found)
		}
		record, err := storage.FindDocument(ctx, DocumentAddress{Kind: proto, Scope: DocumentScope{Kind: ScopeSession}}, CurrentDocumentPoint())
		if err != nil || record == nil || record.ID != firstKindDoc {
			t.Fatalf("proto kind = %+v, %v", record, err)
		}
		record, err = storage.FindDocument(ctx, DocumentAddress{Kind: constructor, Scope: DocumentScope{Kind: ScopeSession}}, CurrentDocumentPoint())
		if err != nil || record == nil || record.ID != secondKindDoc {
			t.Fatalf("constructor kind = %+v, %v", record, err)
		}
		record, err = storage.FindDocument(ctx, DocumentAddress{Kind: "family", Scope: DocumentScope{Kind: ScopeSession}, Key: &proto}, CurrentDocumentPoint())
		if err != nil || record == nil || record.ID != firstKeyDoc {
			t.Fatalf("proto key = %+v, %v", record, err)
		}
		record, err = storage.FindDocument(ctx, DocumentAddress{Kind: "family", Scope: DocumentScope{Kind: ScopeSession}, Key: &constructor}, CurrentDocumentPoint())
		if err != nil || record == nil || record.ID != secondKeyDoc {
			t.Fatalf("constructor key = %+v, %v", record, err)
		}
		documents, err := storage.ScanDocuments(ctx, DocumentQuery{Scope: DocumentScope{Kind: ScopeSession}, At: CurrentDocumentPoint(), Kind: &proto}, nil, 10)
		if err != nil || len(documents.Items) != 1 || documents.Items[0].ID != firstKindDoc {
			t.Fatalf("proto kind scan = %+v, %v", documents.Items, err)
		}
	})
}

// TestConformanceGlobalRecordIDNamespaceAndExhaustedMinting covers upstream's
// "keeps one global record ID namespace and rejects exhausted ID minting".
func TestConformanceGlobalRecordIDNamespaceAndExhaustedMinting(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		mustCommit(t, storage, conversationWrite(RootConversationID))
		explicit := Id(100)
		mustCommit(t, storage, StorageWrite{Type: "entry", Entry: testEntry(explicit, RootConversationID, "message", nil)})
		minted, err := storage.MintID(ctx)
		if err != nil || minted != 101 {
			t.Fatalf("minted = %d, %v", minted, err)
		}
		// A different record type cannot reuse the id.
		if _, err := storage.Commit(ctx, []StorageWrite{
			{Type: "task", Task: pendingTaskRecord(explicit, RootConversationID)},
		}); err == nil || !strings.Contains(err.Error(), "already belongs to entry") {
			t.Fatalf("reuse = %v", err)
		}
		mustCommit(t, storage, StorageWrite{Type: "entry", Entry: testEntry(MaxSafeInteger, RootConversationID, "last-id", nil)})
		if _, err := storage.MintID(ctx); err == nil || !strings.Contains(err.Error(), "ID space is exhausted") {
			t.Fatalf("exhausted = %v", err)
		}
		if _, err := storage.MintID(ctx); err == nil || !strings.Contains(err.Error(), "ID space is exhausted") {
			t.Fatalf("exhausted again = %v", err)
		}
	})
}

// documentCreateWrite builds one document.create write.
func documentCreateWrite(id Id, kind string, key *string, scope DocumentScope, value json.RawMessage, semantics ...*string) StorageWrite {
	record := &DocumentCreate{ID: id, Kind: kind, Key: key, Scope: scope}
	if len(semantics) == 2 {
		record.History = semantics[0]
		record.Fork = semantics[1]
	}
	return StorageWrite{
		Type: "document.create", DocumentCreate: record,
		DocumentContent: &DocumentContent{Version: 1, Kind: ContentBase, Value: value},
	}
}

var _ = errors.New
