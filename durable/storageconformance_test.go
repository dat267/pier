package durable

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
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

// TestConformanceReservesRootID covers upstream's "reserves ID 1 for the
// immutable root conversation".
func TestConformanceReservesRootID(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		id := mustMintID(t, storage)
		if id != 2 {
			t.Fatalf("first minted id = %d, want 2", id)
		}
		mustCommit(t, storage, conversationWrite(RootConversationID))
		record, err := storage.Conversation(ctx, RootConversationID)
		if err != nil || record == nil || record.ID != RootConversationID {
			t.Fatalf("record = %+v, %v", record, err)
		}
		_, err = storage.Commit(ctx, []StorageWrite{{
			Type: "conversation", Conversation: &ConversationRecord{ID: RootConversationID},
		}})
		if err == nil || !strings.Contains(err.Error(), "already belongs to conversation") {
			t.Fatalf("err = %v", err)
		}
	})
}

// TestConformanceMixedWritesAtomic covers upstream's "commits mixed table
// writes atomically and rolls all of them back on failure".
func TestConformanceMixedWritesAtomic(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		mustCommit(t, storage, conversationWrite(RootConversationID))
		entryID := mustMintID(t, storage)
		taskID := mustMintID(t, storage)
		submissionID := mustMintID(t, storage)
		task := pendingTaskRecord(taskID, RootConversationID)
		input := &SubmissionRecord{
			ID: submissionID, ConversationID: RootConversationID, RequestID: stringPointer("request-1"),
			Type: SubmissionTypeInput, Status: SubmissionPlaced, Entry: &entryID,
		}
		initialSeq := mustCommit(t, storage,
			StorageWrite{Type: "entry", Entry: &EntryRecord{ID: entryID, ConversationID: RootConversationID, Kind: "user", Data: dataJSON(map[string]any{"text": "hello"})}},
			StorageWrite{Type: "task", Task: task},
			StorageWrite{Type: "submission", Submission: input},
		)
		commit, err := storage.Entry(ctx, entryID)
		if err != nil || commit == nil || commit.CommitSeq != initialSeq {
			t.Fatalf("entry = %+v, %v", commit, err)
		}
		transientID := mustMintID(t, storage)
		running := *task
		running.State = TaskState{Status: TaskRunning, Checkpoint: dataJSON(map[string]any{"phase": "effect"})}
		done := *input
		done.Status = SubmissionDone
		done.Answer = &transientID
		_, err = storage.Commit(ctx, []StorageWrite{
			{Type: "task", Task: &running},
			{Type: "submission", Submission: &done},
			{Type: "entry", Entry: &EntryRecord{ID: transientID, ConversationID: RootConversationID, Kind: "assistant"}},
			{Type: "conversation", Conversation: &ConversationRecord{ID: RootConversationID}},
		})
		if err == nil {
			t.Fatal("a duplicate conversation must fail the commit")
		}
		storedTask, _ := storage.Task(ctx, taskID)
		if storedTask == nil || storedTask.State.Status != TaskPending {
			t.Fatalf("task = %+v", storedTask)
		}
		storedInput, _ := storage.Submission(ctx, submissionID)
		if storedInput == nil || storedInput.Status != SubmissionPlaced {
			t.Fatalf("submission = %+v", storedInput)
		}
		if entry, _ := storage.Entry(ctx, transientID); entry != nil {
			t.Fatalf("transient entry = %+v", entry)
		}
		afterSeq := mustCommit(t, storage, StorageWrite{
			Type: "entry", Entry: &EntryRecord{ID: mustMintID(t, storage), ConversationID: RootConversationID, Kind: "after-rollback"},
		})
		if afterSeq <= initialSeq {
			t.Fatalf("afterSeq = %d, initialSeq = %d", afterSeq, initialSeq)
		}
	})
}

// TestConformanceDetachesRecords covers upstream's "detaches retained writes
// and every returned record".
func TestConformanceDetachesRecords(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		mustCommit(t, storage, conversationWrite(RootConversationID))
		entryID := mustMintID(t, storage)
		source := []byte(`{"nested":[1,2]}`)
		mustCommit(t, storage, StorageWrite{Type: "entry", Entry: &EntryRecord{
			ID: entryID, ConversationID: RootConversationID, Kind: "note", Data: source,
		}})
		// Mutating the source bytes after the commit does not change the store.
		for index := range source {
			source[index] = 'x'
		}
		commit, err := storage.Entry(ctx, entryID)
		if err != nil || commit == nil || string(commit.Entry.Data) != `{"nested":[1,2]}` {
			t.Fatalf("entry = %+v, %v", commit, err)
		}
		// Mutating the returned record does not change the store either.
		commit.Entry.Data[0] = 'x'
		again, _ := storage.Entry(ctx, entryID)
		if again == nil || string(again.Entry.Data) != `{"nested":[1,2]}` {
			t.Fatalf("entry = %+v", again)
		}
	})
}

// TestConformancePrototypeKeys covers upstream's "detaches prototype-like JSON
// keys without changing object prototypes".
func TestConformancePrototypeKeys(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		mustCommit(t, storage, conversationWrite(RootConversationID))
		entryID := mustMintID(t, storage)
		data := json.RawMessage(`{"__proto__":{"polluted":false},"constructor":{"label":"stored"},"toString":"value"}`)
		mustCommit(t, storage, StorageWrite{Type: "entry", Entry: &EntryRecord{
			ID: entryID, ConversationID: RootConversationID, Kind: "note", Data: data,
		}})
		commit, err := storage.Entry(ctx, entryID)
		if err != nil || commit == nil {
			t.Fatalf("entry = %+v, %v", commit, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(commit.Entry.Data, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["toString"] != "value" {
			t.Fatalf("data = %+v", decoded)
		}
		if _, present := decoded["constructor"]; !present {
			t.Fatalf("data = %+v", decoded)
		}
	})
}

// TestConformancePaginatesConversationsByCursor covers upstream's "paginates
// conversations by opaque cursor in ascending ID order".
func TestConformancePaginatesConversationsByCursor(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		mustCommit(t, storage, conversationWrite(RootConversationID))
		secondID := mustMintID(t, storage)
		thirdID := mustMintID(t, storage)
		mustCommit(t, storage,
			StorageWrite{Type: "conversation", Conversation: &ConversationRecord{ID: thirdID}},
			StorageWrite{Type: "conversation", Conversation: &ConversationRecord{ID: secondID}},
		)
		first, err := storage.ScanConversations(ctx, ConversationQuery{}, nil, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(first.Items) != 2 || first.Items[0].ID != RootConversationID || first.Items[1].ID != secondID {
			t.Fatalf("first = %+v", first.Items)
		}
		if first.Next == nil {
			t.Fatal("the first page must have a cursor")
		}
		encoded, err := json.Marshal(first.Next)
		if err != nil {
			t.Fatal(err)
		}
		var cursor Cursor
		if err := json.Unmarshal(encoded, &cursor); err != nil {
			t.Fatal(err)
		}
		second, err := storage.ScanConversations(ctx, ConversationQuery{}, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(second.Items) != 1 || second.Items[0].ID != thirdID {
			t.Fatalf("second = %+v", second.Items)
		}
		if second.Next != nil {
			t.Fatalf("second cursor = %+v", second.Next)
		}
	})
}

// Port of "scans tables in either ID order and continues a cursor in its order"
// from packages/durable/src/testing/storage-conformance.ts at pi v1.1.0 commit 4dd2af42c.
func TestConformanceScansConversationsDescendingAndContinuesCursorOrder(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		for _, id := range []Id{2, 4, 6, 8} {
			mustCommit(t, storage, conversationWrite(id))
		}
		first, err := storage.ScanConversations(ctx, ConversationQuery{Order: ScanOrderDescending}, nil, 2)
		if err != nil || len(first.Items) != 2 || first.Items[0].ID != 8 || first.Items[1].ID != 6 || first.Next == nil {
			t.Fatalf("descending first page = %+v, %v", first, err)
		}
		encoded, err := json.Marshal(first.Next)
		if err != nil {
			t.Fatal(err)
		}
		var cursor Cursor
		if err := json.Unmarshal(encoded, &cursor); err != nil {
			t.Fatal(err)
		}
		second, err := storage.ScanConversations(ctx, ConversationQuery{}, cursor, 2)
		if err != nil || len(second.Items) != 2 || second.Items[0].ID != 4 || second.Items[1].ID != 2 || second.Next != nil {
			t.Fatalf("cursor continuation = %+v, %v", second, err)
		}
	})
}

// D213 regression for the SQLite first-page sentinel in packages/durable/src/storage/sqlite/storage.ts.
func TestConformanceScansIntegerBoundaryIDsInBothOrders(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		for _, id := range []Id{2, MaxSafeInteger} {
			mustCommit(t, storage, conversationWrite(id))
		}
		ascending, err := storage.ScanConversations(context.Background(), ConversationQuery{Order: ScanOrderAscending}, nil, 2)
		if err != nil || len(ascending.Items) != 2 || ascending.Items[0].ID != 2 || ascending.Items[1].ID != MaxSafeInteger {
			t.Fatalf("ascending boundary IDs = %+v, %v", ascending.Items, err)
		}
		descending, err := storage.ScanConversations(context.Background(), ConversationQuery{Order: ScanOrderDescending}, nil, 2)
		if err != nil || len(descending.Items) != 2 || descending.Items[0].ID != MaxSafeInteger || descending.Items[1].ID != 2 {
			t.Fatalf("descending boundary IDs = %+v, %v", descending.Items, err)
		}
	})
}

// Port of "scans tables in either ID order and continues a cursor in its order"
// from packages/durable/src/testing/storage-conformance.ts at pi v1.1.0 commit 4dd2af42c.
func TestConformanceScansTasksDescendingAndContinuesCursorOrder(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		conversationID := Id(2)
		mustCommit(t, storage, conversationWrite(conversationID))
		for _, id := range []Id{10, 12, 14, 16} {
			mustCommit(t, storage, StorageWrite{Type: "task", Task: pendingTaskRecord(id, conversationID)})
		}
		first, err := storage.ScanTasks(ctx, TaskQuery{Order: ScanOrderDescending}, nil, 2)
		if err != nil || len(first.Items) != 2 || first.Items[0].ID != 16 || first.Items[1].ID != 14 || first.Next == nil {
			t.Fatalf("descending first page = %+v, %v", first, err)
		}
		second, err := storage.ScanTasks(ctx, TaskQuery{}, first.Next, 2)
		if err != nil || len(second.Items) != 2 || second.Items[0].ID != 12 || second.Items[1].ID != 10 || second.Next != nil {
			t.Fatalf("cursor continuation = %+v, %v", second, err)
		}
	})
}

// Port of "scans tables in either ID order and continues a cursor in its order"
// from packages/durable/src/testing/storage-conformance.ts at pi v1.1.0 commit 4dd2af42c.
func TestConformanceScansSubmissionsDescendingAndContinuesCursorOrder(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		conversationID := Id(2)
		mustCommit(t, storage, conversationWrite(conversationID))
		for _, id := range []Id{10, 12, 14, 16} {
			mustCommit(t, storage, StorageWrite{Type: "submission", Submission: &SubmissionRecord{
				ID: id, ConversationID: conversationID, Type: SubmissionTypeInput, Status: SubmissionQueued,
			}})
		}
		first, err := storage.ScanSubmissions(ctx, SubmissionQuery{Order: ScanOrderDescending}, nil, 2)
		if err != nil || len(first.Items) != 2 || first.Items[0].ID != 16 || first.Items[1].ID != 14 || first.Next == nil {
			t.Fatalf("descending first page = %+v, %v", first, err)
		}
		second, err := storage.ScanSubmissions(ctx, SubmissionQuery{}, first.Next, 2)
		if err != nil || len(second.Items) != 2 || second.Items[0].ID != 12 || second.Items[1].ID != 10 || second.Next != nil {
			t.Fatalf("cursor continuation = %+v, %v", second, err)
		}
	})
}

// Port of ascending fork-history cases from
// packages/durable/src/testing/storage-conformance.ts at pi v1.1.0 commit 4dd2af42c.
func TestConformanceScansForkHistoryAscending(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		root, child, grandchild := Id(2), Id(30), Id(50)
		mustCommit(t, storage,
			conversationWrite(root),
			StorageWrite{Type: "entry", Entry: testEntry(10, root, "message", nil)},
			StorageWrite{Type: "entry", Entry: testEntry(20, root, "message", nil)},
			StorageWrite{Type: "entry", Entry: testEntry(25, root, "message", nil)},
		)
		mustCommit(t, storage,
			StorageWrite{Type: "conversation", Conversation: &ConversationRecord{
				ID: child, Parent: &ConversationParent{ConversationID: root, At: 20},
			}},
			StorageWrite{Type: "entry", Entry: testEntry(40, child, "message", nil)},
			StorageWrite{Type: "entry", Entry: testEntry(45, child, "message", nil)},
		)
		mustCommit(t, storage,
			StorageWrite{Type: "conversation", Conversation: &ConversationRecord{
				ID: grandchild, Parent: &ConversationParent{ConversationID: child, At: 40},
			}},
			StorageWrite{Type: "entry", Entry: testEntry(60, grandchild, "message", nil)},
		)
		first, err := storage.ScanEntries(ctx, EntryQuery{ConversationID: grandchild, Order: ScanOrderAscending}, nil, 2)
		if err != nil || !sameIDs(first.Items, []Id{10, 20}) || first.Next == nil {
			t.Fatalf("ascending first page = %+v, %v", first.Items, err)
		}
		second, err := storage.ScanEntries(ctx, EntryQuery{ConversationID: grandchild}, first.Next, 2)
		if err != nil || !sameIDs(second.Items, []Id{40, 60}) || second.Next != nil {
			t.Fatalf("ascending cursor continuation = %+v, %v", second.Items, err)
		}
		minEntry, maxEntry := Id(20), Id(40)
		bounded, err := storage.ScanEntries(ctx, EntryQuery{
			ConversationID: grandchild, MinEntryID: &minEntry, MaxEntryID: &maxEntry, Order: ScanOrderAscending,
		}, nil, 10)
		if err != nil || !sameIDs(bounded.Items, []Id{20, 40}) {
			t.Fatalf("ascending bounded history = %+v, %v", bounded.Items, err)
		}
	})
}

// Port of cursor validation from packages/durable/src/storage/scan.ts at pi v1.1.0
// commit 4dd2af42c.
func TestConformanceRejectsNonNumericCursorID(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		mustCommit(t, storage, conversationWrite(2))
		cursor := Cursor{"after": json.RawMessage(`"2"`)}
		_, err := storage.ScanConversations(context.Background(), ConversationQuery{}, cursor, 1)
		if err == nil || !strings.Contains(err.Error(), "Invalid storage cursor") {
			t.Fatalf("invalid cursor ID error = %v", err)
		}
	})
}

// Port of legacy cursor fallback from packages/durable/src/storage/scan.ts at pi v1.1.0
// commit 4dd2af42c.
func TestConformanceLegacyCursorsUseDefaultScanOrder(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		for _, id := range []Id{2, 4, 6} {
			mustCommit(t, storage, conversationWrite(id))
		}
		mustCommit(t, storage,
			StorageWrite{Type: "entry", Entry: testEntry(10, 2, "message", nil)},
			StorageWrite{Type: "entry", Entry: testEntry(20, 2, "message", nil)},
			StorageWrite{Type: "entry", Entry: testEntry(30, 2, "message", nil)},
		)
		conversations, err := storage.ScanConversations(ctx, ConversationQuery{}, CursorAfter(2), 2)
		if err != nil || len(conversations.Items) != 2 || conversations.Items[0].ID != 4 || conversations.Items[1].ID != 6 {
			t.Fatalf("legacy conversation cursor = %+v, %v", conversations.Items, err)
		}
		entries, err := storage.ScanEntries(ctx, EntryQuery{ConversationID: 2}, CursorAfter(20), 2)
		if err != nil || !sameIDs(entries.Items, []Id{10}) {
			t.Fatalf("legacy entry cursor = %+v, %v", entries.Items, err)
		}
	})
}

// Port of cursor validation from packages/durable/src/storage/scan.ts at pi v1.1.0
// commit 4dd2af42c.
func TestConformanceRejectsInvalidCursorOrder(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		mustCommit(t, storage, conversationWrite(2))
		cursor := Cursor{"after": json.RawMessage(`2`), "order": json.RawMessage(`"sideways"`)}
		_, err := storage.ScanConversations(context.Background(), ConversationQuery{}, cursor, 1)
		if err == nil || !strings.Contains(err.Error(), "Invalid storage cursor") {
			t.Fatalf("invalid cursor error = %v", err)
		}
	})
}

// Port of scan-order validation from packages/durable/src/storage/scan.ts at pi v1.1.0
// commit 4dd2af42c.
func TestConformanceRejectsInvalidScanOrder(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		mustCommit(t, storage, conversationWrite(2))
		_, err := storage.ScanConversations(context.Background(), ConversationQuery{Order: ScanOrder("sideways")}, nil, 1)
		if err == nil || !strings.Contains(err.Error(), "Invalid scan order") {
			t.Fatalf("invalid scan order error = %v", err)
		}
	})
}

// Port of cursor-order mismatch validation from packages/durable/src/storage/scan.ts at pi v1.1.0
// commit 4dd2af42c.
func TestConformanceRejectsCursorWithDifferentScanOrder(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		for _, id := range []Id{2, 4, 6} {
			mustCommit(t, storage, conversationWrite(id))
		}
		first, err := storage.ScanConversations(ctx, ConversationQuery{Order: ScanOrderDescending}, nil, 1)
		if err != nil || first.Next == nil {
			t.Fatalf("first page = %+v, %v", first, err)
		}
		_, err = storage.ScanConversations(ctx, ConversationQuery{Order: ScanOrderAscending}, first.Next, 1)
		if err == nil || !strings.Contains(err.Error(), "cursor") {
			t.Fatalf("mismatched cursor order error = %v", err)
		}
	})
}

// Port of persisted lifecycle times from packages/durable/test/harness-tasks-recovery.test.ts
// at pi v1.1.0 commit 36a686ee8.
func TestConformancePersistsTaskLifecycleTimes(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		mustCommit(t, storage, conversationWrite(RootConversationID))
		startedAt, endedAt := int64(1_000), int64(2_000)
		task := pendingTaskRecord(7, RootConversationID)
		task.State = TaskState{Status: TaskTerminal, Outcome: &TaskOutcome{Status: OutcomeCompleted}}
		task.StartedAt, task.EndedAt = &startedAt, &endedAt
		mustCommit(t, storage, StorageWrite{Type: "task", Task: task})
		loaded, err := storage.Task(ctx, task.ID)
		if err != nil || loaded == nil || loaded.StartedAt == nil || *loaded.StartedAt != startedAt ||
			loaded.EndedAt == nil || *loaded.EndedAt != endedAt {
			t.Fatalf("persisted task times = %+v, %v", loaded, err)
		}
	})
}

// TestConformanceRejectsAfterClose covers upstream's "rejects every operation
// after close".
func TestConformanceRejectsAfterClose(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		mustCommit(t, storage, conversationWrite(RootConversationID))
		if err := storage.Close(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := storage.Conversation(ctx, RootConversationID); err == nil ||
			!strings.Contains(err.Error(), "closed") {
			t.Fatalf("conversation err = %v", err)
		}
		if _, err := storage.Commit(ctx, []StorageWrite{}); err == nil ||
			!strings.Contains(err.Error(), "closed") {
			t.Fatalf("commit err = %v", err)
		}
		if _, err := storage.MintID(ctx); err == nil || !strings.Contains(err.Error(), "closed") {
			t.Fatalf("mint err = %v", err)
		}
	})
}

// TestConformanceDeepForkHistory covers upstream's "scans deep fork history
// newest-first through every ancestor cap".
func TestConformanceDeepForkHistory(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		mustCommit(t, storage, conversationWrite(RootConversationID))
		rootFirst := mustMintID(t, storage)
		rootForkPoint := mustMintID(t, storage)
		rootExcludedSameCommit := mustMintID(t, storage)
		rootSeq := mustCommit(t, storage,
			StorageWrite{Type: "entry", Entry: &EntryRecord{ID: rootFirst, ConversationID: RootConversationID, Kind: "message"}},
			StorageWrite{Type: "entry", Entry: &EntryRecord{ID: rootForkPoint, ConversationID: RootConversationID, Kind: "marker", Head: &rootFirst}},
			StorageWrite{Type: "entry", Entry: &EntryRecord{ID: rootExcludedSameCommit, ConversationID: RootConversationID, Kind: "message"}},
		)
		childID := mustMintID(t, storage)
		mustCommit(t, storage, StorageWrite{Type: "conversation", Conversation: &ConversationRecord{
			ID: childID, Parent: &ConversationParent{ConversationID: RootConversationID, At: rootForkPoint},
		}})
		childForkPoint := mustMintID(t, storage)
		childExcluded := mustMintID(t, storage)
		mustCommit(t, storage,
			StorageWrite{Type: "entry", Entry: &EntryRecord{ID: childForkPoint, ConversationID: childID, Kind: "note"}},
			StorageWrite{Type: "entry", Entry: &EntryRecord{ID: childExcluded, ConversationID: childID, Kind: "message"}},
		)
		rootExcludedLater := mustMintID(t, storage)
		mustCommit(t, storage, StorageWrite{Type: "entry", Entry: &EntryRecord{ID: rootExcludedLater, ConversationID: RootConversationID, Kind: "message"}})
		grandchildID := mustMintID(t, storage)
		mustCommit(t, storage, StorageWrite{Type: "conversation", Conversation: &ConversationRecord{
			ID: grandchildID, Parent: &ConversationParent{ConversationID: childID, At: childForkPoint},
		}})
		grandchildHead := mustMintID(t, storage)
		grandchildTail := mustMintID(t, storage)
		grandchildSeq := mustCommit(t, storage,
			StorageWrite{Type: "entry", Entry: &EntryRecord{ID: grandchildHead, ConversationID: grandchildID, Kind: "marker", Head: &grandchildHead}},
			StorageWrite{Type: "entry", Entry: &EntryRecord{ID: grandchildTail, ConversationID: grandchildID, Kind: "message"}},
		)
		childExcludedLater := mustMintID(t, storage)
		mustCommit(t, storage, StorageWrite{Type: "entry", Entry: &EntryRecord{ID: childExcludedLater, ConversationID: childID, Kind: "message"}})

		first := scanEntryIDs(t, storage, EntryQuery{ConversationID: grandchildID}, 2, nil)
		if !sameIDs(first.Items, []Id{grandchildTail, grandchildHead}) {
			t.Fatalf("first = %+v", first.Items)
		}
		second := scanEntryIDs(t, storage, EntryQuery{ConversationID: grandchildID}, 2, first.Next)
		if !sameIDs(second.Items, []Id{childForkPoint, rootForkPoint}) {
			t.Fatalf("second = %+v", second.Items)
		}
		third := scanEntryIDs(t, storage, EntryQuery{ConversationID: grandchildID}, 2, second.Next)
		if !sameIDs(third.Items, []Id{rootFirst}) || third.Next != nil {
			t.Fatalf("third = %+v", third)
		}
		// Head markers resolve at and before the tail.
		current, err := storage.FindLatestHeadMarker(ctx, grandchildID, nil)
		if err != nil || current == nil || current.ID != grandchildHead || current.Head == nil || *current.Head != grandchildHead {
			t.Fatalf("current marker = %+v, %v", current, err)
		}
		historical, err := storage.FindLatestHeadMarker(ctx, grandchildID, &childForkPoint)
		if err != nil || historical == nil || historical.ID != rootForkPoint || historical.Head == nil || *historical.Head != rootFirst {
			t.Fatalf("historical marker = %+v, %v", historical, err)
		}
		if marker, _ := storage.FindLatestHeadMarker(ctx, grandchildID, &rootFirst); marker != nil {
			t.Fatalf("marker = %+v", marker)
		}
		// The active range starts at the marker's head.
		activeFirst := scanEntryIDs(t, storage, EntryQuery{ConversationID: grandchildID, MinEntryID: current.Head}, 1, nil)
		if !sameIDs(activeFirst.Items, []Id{grandchildTail}) || activeFirst.Next == nil {
			t.Fatalf("activeFirst = %+v", activeFirst)
		}
		activeSecond := scanEntryIDs(t, storage, EntryQuery{ConversationID: grandchildID, MinEntryID: current.Head}, 1, activeFirst.Next)
		if !sameIDs(activeSecond.Items, []Id{grandchildHead}) || activeSecond.Next != nil {
			t.Fatalf("activeSecond = %+v", activeSecond)
		}
		historicalRange := scanEntryIDs(t, storage, EntryQuery{
			ConversationID: grandchildID, MinEntryID: historical.Head, MaxEntryID: &childForkPoint,
		}, 10, nil)
		if !sameIDs(historicalRange.Items, []Id{childForkPoint, rootForkPoint, rootFirst}) {
			t.Fatalf("historicalRange = %+v", historicalRange.Items)
		}
		// Commit sequences match the commits that persisted each entry.
		if commit, _ := storage.Entry(ctx, rootForkPoint); commit == nil || commit.CommitSeq != rootSeq {
			t.Fatalf("root fork point = %+v", commit)
		}
		if commit, _ := storage.Entry(ctx, grandchildTail); commit == nil || commit.CommitSeq != grandchildSeq {
			t.Fatalf("grandchild tail = %+v", commit)
		}
		// Conversation-scoped visibility follows the fork chain.
		if commit, _ := visibleEntry(ctx, storage, grandchildID, rootFirst); commit == nil || commit.Entry.ConversationID != RootConversationID {
			t.Fatalf("root first = %+v", commit)
		}
		if commit, _ := visibleEntry(ctx, storage, grandchildID, childForkPoint); commit == nil || commit.Entry.ConversationID != childID {
			t.Fatalf("child fork point = %+v", commit)
		}
		for _, hidden := range []Id{rootExcludedSameCommit, rootExcludedLater, childExcluded, childExcludedLater} {
			if commit, _ := visibleEntry(ctx, storage, grandchildID, hidden); commit != nil {
				t.Fatalf("hidden %d = %+v", hidden, commit)
			}
		}
	})
}

func scanEntryIDs(t *testing.T, storage Storage, query EntryQuery, limit int, cursor Cursor) Page[EntryRecord] {
	t.Helper()
	page, err := storage.ScanEntries(context.Background(), query, cursor, limit)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func sameIDs(entries []EntryRecord, ids []Id) bool {
	if len(entries) != len(ids) {
		return false
	}
	for index := range entries {
		if entries[index].ID != ids[index] {
			return false
		}
	}
	return true
}

type longTailRow struct {
	Value  int    `json:"value"`
	Stable string `json:"stable"`
}

type longTail struct {
	Revision int           `json:"revision"`
	Rows     []longTailRow `json:"rows"`
}

func buildLongTail(revision, valueBase int, stablePrefix string) longTail {
	rows := make([]longTailRow, 512)
	for index := range rows {
		rows[index] = longTailRow{Value: valueBase + index, Stable: stablePrefix + strconv.Itoa(index)}
	}
	return longTail{Revision: revision, Rows: rows}
}

func cloneLongTail(value longTail) longTail {
	rows := make([]longTailRow, len(value.Rows))
	copy(rows, value.Rows)
	return longTail{Revision: value.Revision, Rows: rows}
}

func longTailAt(t *testing.T, storage Storage, id Id, point DocumentPoint) longTail {
	t.Helper()
	stored, err := storage.Document(context.Background(), id, point)
	if err != nil || stored == nil {
		t.Fatalf("document = %+v, %v", stored, err)
	}
	var value longTail
	if err := json.Unmarshal(stored.Value, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

// TestConformanceLongDocumentTails covers upstream's "streams long document
// tails across root replacement deltas".
func TestConformanceLongDocumentTails(t *testing.T) {
	documentBackends(t, func(t *testing.T, storage Storage) {
		mustCommit(t, storage, conversationWrite(RootConversationID))
		root := RootConversationID
		id := mustMintID(t, storage)
		history, fork := HistoryRewindable, ForkAsOf
		initial := buildLongTail(0, 0, "row-")
		initialValue, err := jsonValueOf(initial)
		if err != nil {
			t.Fatal(err)
		}
		createdAt := mustCommit(t, storage, StorageWrite{
			Type: "document.create",
			DocumentCreate: &DocumentCreate{ID: id, Kind: "conversation.long-tail",
				Scope: DocumentScope{Kind: ScopeConversation, ConversationID: &root}, History: &history, Fork: &fork},
			DocumentContent: &DocumentContent{Version: 1, Kind: ContentBase, Value: json.RawMessage(string(dataJSON(initialValue)))},
		})
		before := buildLongTail(0, 0, "row-")
		beforeAt := DocumentPointAt(createdAt)
		for revision := 1; revision <= 24; revision++ {
			index := (revision * 17) % len(before.Rows)
			before.Rows[index].Value = -revision
			before.Revision = revision
			beforeAt = DocumentPointAt(mustCommit(t, storage, StorageWrite{
				Type: "document.change", DocumentID: &id,
				DocumentContent: &DocumentContent{Version: 1, Kind: ContentDelta, Ops: []any{
					[]any{"s", []any{"rows", index, "value"}, -revision},
					[]any{"s", []any{"revision"}, revision},
				}},
			}))
		}
		replacement := buildLongTail(100, 10_000, "new-")
		replacementSnapshot := cloneLongTail(replacement)
		replacementValue, err := jsonValueOf(replacement)
		if err != nil {
			t.Fatal(err)
		}
		replacementAt := DocumentPointAt(mustCommit(t, storage, StorageWrite{
			Type: "document.change", DocumentID: &id,
			DocumentContent: &DocumentContent{Version: 1, Kind: ContentDelta, Ops: []any{
				[]any{"r", replacementValue},
			}},
		}))
		// Mutating the source after the commit does not change the stored value.
		replacement.Rows[0].Value = -999
		current := cloneLongTail(replacementSnapshot)
		for revision := 101; revision <= 124; revision++ {
			index := (revision * 19) % len(current.Rows)
			current.Rows[index].Value = -revision
			current.Revision = revision
			mustCommit(t, storage, StorageWrite{
				Type: "document.change", DocumentID: &id,
				DocumentContent: &DocumentContent{Version: 1, Kind: ContentDelta, Ops: []any{
					[]any{"s", []any{"rows", index, "value"}, -revision},
					[]any{"s", []any{"revision"}, revision},
				}},
			})
		}
		if got := longTailAt(t, storage, id, DocumentPointAt(createdAt)); !reflect.DeepEqual(got, initial) {
			t.Fatalf("initial = %+v", got.Revision)
		}
		if got := longTailAt(t, storage, id, beforeAt); !reflect.DeepEqual(got, before) {
			t.Fatalf("before = %+v", got.Revision)
		}
		if got := longTailAt(t, storage, id, replacementAt); !reflect.DeepEqual(got, replacementSnapshot) {
			t.Fatalf("replacement = %+v", got.Revision)
		}
		read := longTailAt(t, storage, id, CurrentDocumentPoint())
		if !reflect.DeepEqual(read, current) {
			t.Fatalf("current = %+v", read.Revision)
		}
		// A returned value is detached from the store.
		read.Rows[0].Value = -1_000
		if again := longTailAt(t, storage, id, CurrentDocumentPoint()); !reflect.DeepEqual(again, current) {
			t.Fatalf("current = %+v", again.Revision)
		}
	})
}

// TestConformanceDocumentVersionTransitions covers upstream's "uses bases for
// version transitions and rejects historical reads of current-only documents".
func TestConformanceDocumentVersionTransitions(t *testing.T) {
	documentBackends(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		mustCommit(t, storage, conversationWrite(RootConversationID))
		id := mustMintID(t, storage)
		mustCommit(t, storage, StorageWrite{
			Type: "document.create",
			DocumentCreate: &DocumentCreate{ID: id, Kind: "session.settings",
				Scope: DocumentScope{Kind: ScopeSession}},
			DocumentContent: &DocumentContent{Version: 1, Kind: ContentBase, Value: json.RawMessage(`{"count":1}`)},
		})
		mustCommit(t, storage, StorageWrite{
			Type: "document.change", DocumentID: &id,
			DocumentContent: &DocumentContent{Version: 1, Kind: ContentDelta, Ops: []any{[]any{"s", []any{"count"}, 2}}},
		})
		migratedAt := mustCommit(t, storage, StorageWrite{
			Type: "document.change", DocumentID: &id,
			DocumentContent: &DocumentContent{Version: 2, Kind: ContentBase, Value: json.RawMessage(`{"count":3}`)},
		})
		current, err := storage.Document(ctx, id, CurrentDocumentPoint())
		if err != nil || current == nil || current.Version != 2 || documentValue(t, current)["count"] != 3.0 {
			t.Fatalf("current = %+v, %v", current, err)
		}
		// A current-only (latest history) document rejects a historical read.
		if _, err := storage.Document(ctx, id, DocumentPointAt(migratedAt)); err == nil ||
			!strings.Contains(err.Error(), "does not retain historical content") {
			t.Fatalf("historical err = %v", err)
		}
		// A delta from an older version without a base is rejected.
		if _, err := storage.Commit(ctx, []StorageWrite{{
			Type: "document.change", DocumentID: &id,
			DocumentContent: &DocumentContent{Version: 1, Kind: ContentDelta, Ops: []any{[]any{"s", []any{"count"}, 4}}},
		}}); err == nil || !strings.Contains(err.Error(), "version transition requires a base") {
			t.Fatalf("transition err = %v", err)
		}
		current, _ = storage.Document(ctx, id, CurrentDocumentPoint())
		if documentValue(t, current)["count"] != 3.0 {
			t.Fatalf("current = %v", documentValue(t, current))
		}
		mustCommit(t, storage, StorageWrite{Type: "document.retire", DocumentID: &id})
		if retired, _ := storage.Document(ctx, id, CurrentDocumentPoint()); retired != nil {
			t.Fatalf("retired = %+v", retired)
		}
	})
}

// TestConformanceDocumentLifecycleAtomic covers upstream's "keeps document
// lifecycle failures atomic and gives create-plus-retire an empty lifetime".
func TestConformanceDocumentLifecycleAtomic(t *testing.T) {
	documentBackends(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		mustCommit(t, storage, conversationWrite(RootConversationID))
		firstID := mustMintID(t, storage)
		secondID := mustMintID(t, storage)
		record := &DocumentCreate{ID: firstID, Kind: "singleton", Scope: DocumentScope{Kind: ScopeSession}}
		mustCommit(t, storage, StorageWrite{
			Type: "document.create", DocumentCreate: record,
			DocumentContent: &DocumentContent{Version: 1, Kind: ContentBase, Value: json.RawMessage(`{"value":1}`)},
		})
		// A second incarnation at the same address fails the whole commit.
		_, err := storage.Commit(ctx, []StorageWrite{
			{Type: "document.create", DocumentCreate: &DocumentCreate{ID: secondID, Kind: "singleton", Scope: DocumentScope{Kind: ScopeSession}},
				DocumentContent: &DocumentContent{Version: 1, Kind: ContentBase, Value: json.RawMessage(`{"value":2}`)}},
			{Type: "document.change", DocumentID: &firstID,
				DocumentContent: &DocumentContent{Version: 1, Kind: ContentDelta, Ops: []any{}}},
		})
		if err == nil || !strings.Contains(err.Error(), "already has a current incarnation") {
			t.Fatalf("err = %v", err)
		}
		first, _ := storage.Document(ctx, firstID, CurrentDocumentPoint())
		if first == nil || documentValue(t, first)["value"] != 1.0 {
			t.Fatalf("first = %+v", first)
		}
		if second, _ := storage.Document(ctx, secondID, CurrentDocumentPoint()); second != nil {
			t.Fatalf("second = %+v", second)
		}
		// Create plus retire in one commit gives an empty lifetime.
		emptyID := mustMintID(t, storage)
		root := RootConversationID
		key := "empty"
		emptyAt := mustCommit(t, storage, StorageWrite{
			Type: "document.create",
			DocumentCreate: &DocumentCreate{ID: emptyID, Kind: "singleton", Key: &key,
				Scope:   DocumentScope{Kind: ScopeConversation, ConversationID: &[]Id{RootConversationID}[0]},
				History: stringPointer(HistoryRewindable), Fork: stringPointer(ForkInitial)},
			DocumentContent: &DocumentContent{Version: 1, Kind: ContentBase, Value: json.RawMessage(`{}`)},
		}, StorageWrite{Type: "document.retire", DocumentID: &emptyID})
		if empty, _ := storage.Document(ctx, emptyID, CurrentDocumentPoint()); empty != nil {
			t.Fatalf("empty = %+v", empty)
		}
		if empty, _ := storage.Document(ctx, emptyID, DocumentPointAt(emptyAt)); empty != nil {
			t.Fatalf("empty at = %+v", empty)
		}
		address := DocumentAddress{Kind: "singleton", Key: &key,
			Scope: DocumentScope{Kind: ScopeConversation, ConversationID: &root}}
		if found, _ := storage.FindDocument(ctx, address, DocumentPointAt(emptyAt)); found != nil {
			t.Fatalf("found = %+v", found)
		}
	})
}

// TestConformanceDocumentCommandRollback covers upstream's "rolls back record
// tables and secondary indexes when a document command fails".
func TestConformanceDocumentCommandRollback(t *testing.T) {
	conformance(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		mustCommit(t, storage, conversationWrite(RootConversationID))
		taskID := mustMintID(t, storage)
		submissionID := mustMintID(t, storage)
		documentID := mustMintID(t, storage)
		task := pendingTaskRecord(taskID, RootConversationID)
		submission := &SubmissionRecord{ID: submissionID, ConversationID: RootConversationID,
			RequestID: stringPointer("atomic"), Type: SubmissionTypeInput, Status: SubmissionQueued}
		baseline := mustCommit(t, storage,
			StorageWrite{Type: "task", Task: task},
			StorageWrite{Type: "submission", Submission: submission},
			StorageWrite{Type: "document.create",
				DocumentCreate:  &DocumentCreate{ID: documentID, Kind: "atomic", Scope: DocumentScope{Kind: ScopeSession}},
				DocumentContent: &DocumentContent{Version: 1, Kind: ContentBase, Value: json.RawMessage(`{"count":1}`)}},
		)
		entryID := mustMintID(t, storage)
		conflictingID := mustMintID(t, storage)
		running := *task
		running.State = TaskState{Status: TaskRunning, Checkpoint: json.RawMessage(`{"phase":"effect"}`)}
		settled := *submission
		failed := "failed"
		settled.Status = SubmissionUnanswered
		settled.Reason = &failed
		_, err := storage.Commit(ctx, []StorageWrite{
			{Type: "task", Task: &running},
			{Type: "submission", Submission: &settled},
			{Type: "entry", Entry: &EntryRecord{ID: entryID, ConversationID: RootConversationID, Kind: "transient"}},
			{Type: "document.create",
				DocumentCreate:  &DocumentCreate{ID: conflictingID, Kind: "atomic", Scope: DocumentScope{Kind: ScopeSession}},
				DocumentContent: &DocumentContent{Version: 1, Kind: ContentBase, Value: json.RawMessage(`{"count":2}`)}},
		})
		if err == nil || !strings.Contains(err.Error(), "already has a current incarnation") {
			t.Fatalf("err = %v", err)
		}
		storedTask, _ := storage.Task(ctx, taskID)
		if storedTask == nil || !reflect.DeepEqual(storedTask.State, task.State) {
			t.Fatalf("task = %+v", storedTask)
		}
		page, err := storage.ScanTasks(ctx, TaskQuery{Status: stringPointer(TaskPending)}, nil, 10)
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("pending = %+v, %v", page.Items, err)
		}
		storedSubmission, _ := storage.SubmissionByRequest(ctx, RootConversationID, "atomic")
		if storedSubmission == nil || storedSubmission.Status != SubmissionQueued {
			t.Fatalf("submission = %+v", storedSubmission)
		}
		if entry, _ := storage.Entry(ctx, entryID); entry != nil {
			t.Fatalf("entry = %+v", entry)
		}
		if conflicting, _ := storage.Document(ctx, conflictingID, CurrentDocumentPoint()); conflicting != nil {
			t.Fatalf("conflicting = %+v", conflicting)
		}
		found, _ := storage.FindDocument(ctx,
			DocumentAddress{Kind: "atomic", Scope: DocumentScope{Kind: ScopeSession}}, CurrentDocumentPoint())
		if found == nil || found.ID != documentID {
			t.Fatalf("found = %+v", found)
		}
		after := mustCommit(t, storage, StorageWrite{
			Type: "document.change", DocumentID: &documentID,
			DocumentContent: &DocumentContent{Version: 1, Kind: ContentDelta, Ops: []any{[]any{"s", []any{"count"}, 3}}},
		})
		if after <= baseline {
			t.Fatalf("after = %d, baseline = %d", after, baseline)
		}
	})
}
