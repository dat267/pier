package durable

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
)

// Go-only tests for session/transaction.ts. Upstream exercises the transaction
// through the harness suites; the expectations here come from the transaction
// and its public Tx contract.

// testTransactionHost is the minimal TransactionHost the transaction needs.
type testTransactionHost struct {
	storage Storage
	docs    map[string]*LoadedDocument
	created []ConversationRecord
}

func newTestTransactionHost(storage Storage) *testTransactionHost {
	return &testTransactionHost{storage: storage, docs: map[string]*LoadedDocument{}}
}

func (h *testTransactionHost) Storage() Storage { return h.storage }

func (h *testTransactionHost) Cached(addressID string) *LoadedDocument { return h.docs[addressID] }

func (h *testTransactionHost) Load(ctx context.Context, definition DocDefinition, addressID string, address DocumentAddress) (*LoadedDocument, error) {
	if document := h.docs[addressID]; document != nil {
		return document, nil
	}
	record, err := h.storage.FindDocument(ctx, address, CurrentDocumentPoint())
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, nil
	}
	stored, err := h.storage.Document(ctx, record.ID, CurrentDocumentPoint())
	if err != nil {
		return nil, err
	}
	value, err := MaterializeDocument(definition, stored)
	if err != nil {
		return nil, err
	}
	document := &LoadedDocument{
		AddressID: addressID, Record: *record, StoredVersion: stored.Version, ValueVersion: stored.Version,
		DeltasSinceBase: stored.DeltasSinceBase, Tracker: delta.Track(value),
	}
	h.docs[addressID] = document
	return document, nil
}

func (h *testTransactionHost) Install(document LoadedDocument) {
	h.docs[document.AddressID] = &document
}

func (h *testTransactionHost) Evict(addressID string, recordID Id) {
	if document := h.docs[addressID]; document != nil && document.Record.ID == recordID {
		delete(h.docs, addressID)
	}
}

func (h *testTransactionHost) ConversationCreated(tx *Transaction, record ConversationRecord) error {
	h.created = append(h.created, record)
	return nil
}

func sessionDocDefinition() DocDefinition {
	return DocDefinition{
		Kind: "notes", Version: 1, Scope: ScopeSession,
		Initial: func(seed chord.JsonValue) (chord.JsonValue, error) {
			return map[string]any{"count": float64(0)}, nil
		},
	}
}

// mustSettle commits a transaction through settle + storage + adopt.
func mustSettle(t *testing.T, tx *Transaction, host *testTransactionHost) []DocumentCommitChange {
	t.Helper()
	writes, err := tx.SettleSuccess()
	if err != nil {
		t.Fatal(err)
	}
	seq, err := host.storage.Commit(context.Background(), writes)
	if err != nil {
		t.Fatal(err)
	}
	return tx.Adopt(seq)
}

func TestTransactionCreatesRootConversation(t *testing.T) {
	storage := NewMemoryStorage()
	host := newTestTransactionHost(storage)
	tx := NewTransaction(host, context.Background(), TransactionScope{})
	record, err := tx.CreateRootConversation()
	if err != nil {
		t.Fatal(err)
	}
	if record.ID != RootConversationID {
		t.Fatalf("record = %+v", record)
	}
	if len(host.created) != 1 || host.created[0].ID != RootConversationID {
		t.Fatalf("created = %+v", host.created)
	}
	writes, err := tx.SettleSuccess()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Commit(context.Background(), writes); err != nil {
		t.Fatal(err)
	}
	if existing, _ := storage.Conversation(context.Background(), RootConversationID); existing == nil {
		t.Fatal("root conversation not persisted")
	}
}

func TestTransactionRejectsReadAfterWrite(t *testing.T) {
	storage := NewMemoryStorage()
	host := newTestTransactionHost(storage)
	tx := NewTransaction(host, context.Background(), TransactionScope{ConversationID: idPtr(RootConversationID)})
	if _, err := tx.AppendEntry(RootConversationID, EntryDraft{Kind: "message"}); err == nil {
		t.Fatal("append to a missing conversation must fail")
	}
	// Stage the conversation first, then a table read is rejected.
	tx = NewTransaction(host, context.Background(), TransactionScope{})
	if _, err := tx.CreateRootConversation(); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Conversation(RootConversationID); err == nil || !strings.Contains(err.Error(), "cannot read tables") {
		t.Fatalf("read-after-write = %v", err)
	}
}

func TestTransactionAppendEntryStampsAttribution(t *testing.T) {
	storage := NewMemoryStorage()
	host := newTestTransactionHost(storage)
	taskID := Id(9)
	tx := NewTransaction(host, context.Background(), TransactionScope{ConversationID: idPtr(RootConversationID), TaskID: &taskID})
	if _, err := tx.CreateRootConversation(); err != nil {
		t.Fatal(err)
	}
	entry, err := tx.AppendEntry(RootConversationID, EntryDraft{Kind: "message", HeadSelf: true})
	if err != nil {
		t.Fatal(err)
	}
	if entry.ByTaskID == nil || *entry.ByTaskID != taskID || entry.Head == nil || *entry.Head != entry.ID {
		t.Fatalf("entry = %+v", entry)
	}
	writes, err := tx.SettleSuccess()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Commit(context.Background(), writes); err != nil {
		t.Fatal(err)
	}
	stored, err := storage.Entry(context.Background(), entry.ID)
	if err != nil || stored == nil || stored.Entry.Kind != "message" {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
}

func TestTransactionDocumentCreateAndAdopt(t *testing.T) {
	storage := NewMemoryStorage()
	host := newTestTransactionHost(storage)
	tx := NewTransaction(host, context.Background(), TransactionScope{})
	if _, err := tx.CreateRootConversation(); err != nil {
		t.Fatal(err)
	}
	definition := sessionDocDefinition()
	draft, err := tx.Doc(definition)
	if err != nil {
		t.Fatal(err)
	}
	draft.(map[string]any)["count"] = float64(3)
	publications := mustSettle(t, tx, host)
	if len(publications) != 1 || publications[0].Type != "document" {
		t.Fatalf("publications = %+v", publications)
	}
	if encoded, _ := json.Marshal(publications[0].Value); string(encoded) != `{"count":3}` {
		t.Fatalf("value = %s", encoded)
	}
	if publications[0].Version == nil || *publications[0].Version != 1 {
		t.Fatalf("version = %+v", publications[0].Version)
	}
	address := DocumentAddress{Kind: "notes", Scope: DocumentScope{Kind: ScopeSession}}
	loaded := host.Cached(AddressID(address))
	if loaded == nil || loaded.Tracker.Value().(map[string]any)["count"] != float64(3) {
		t.Fatalf("installed = %+v", loaded)
	}
	// The stored incarnation materializes the same value.
	stored, err := storage.FindDocument(context.Background(), address, CurrentDocumentPoint())
	if err != nil || stored == nil {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
	document, err := storage.Document(context.Background(), stored.ID, CurrentDocumentPoint())
	if err != nil || string(document.Value) != `{"count":3}` {
		t.Fatalf("document = %s, %v", document.Value, err)
	}
}

func TestTransactionDocumentLoadedDeltaAndCheckpoint(t *testing.T) {
	storage := NewMemoryStorage()
	host := newTestTransactionHost(storage)
	definition := sessionDocDefinition()
	// Seed one committed incarnation.
	setup := NewTransaction(host, context.Background(), TransactionScope{})
	if _, err := setup.CreateRootConversation(); err != nil {
		t.Fatal(err)
	}
	draft, err := setup.Doc(definition)
	if err != nil {
		t.Fatal(err)
	}
	draft.(map[string]any)["count"] = float64(1)
	mustSettle(t, setup, host)

	// A second transaction loads it and writes a delta.
	tx := NewTransaction(host, context.Background(), TransactionScope{})
	draft, err = tx.Doc(definition)
	if err != nil {
		t.Fatal(err)
	}
	if draft.(map[string]any)["count"] != float64(1) {
		t.Fatalf("loaded draft = %+v", draft)
	}
	draft.(map[string]any)["count"] = float64(2)
	writes, err := tx.SettleSuccess()
	if err != nil {
		t.Fatal(err)
	}
	var change *StorageWrite
	for index := range writes {
		if writes[index].Type == "document.change" {
			change = &writes[index]
		}
	}
	if change == nil || change.DocumentContent.Kind != ContentDelta || len(change.DocumentContent.Ops) == 0 {
		t.Fatalf("change = %+v", change)
	}
	seq, err := storage.Commit(context.Background(), writes)
	if err != nil {
		t.Fatal(err)
	}
	tx.Adopt(seq)
	address := DocumentAddress{Kind: "notes", Scope: DocumentScope{Kind: ScopeSession}}
	loaded := host.Cached(AddressID(address))
	if loaded.Tracker.Value().(map[string]any)["count"] != float64(2) {
		t.Fatalf("loaded value = %+v", loaded.Tracker.Value())
	}
	if loaded.DeltasSinceBase != 1 {
		t.Fatalf("deltas = %d", loaded.DeltasSinceBase)
	}

	// A checkpoint predicate promotes the next delta to a base.
	checkpoint := definition
	checkpoint.CheckpointWhen = func(value chord.JsonValue, ops []delta.Op, info CheckpointInfo) bool { return true }
	tx = NewTransaction(host, context.Background(), TransactionScope{})
	draft, err = tx.Doc(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	draft.(map[string]any)["count"] = float64(3)
	writes, err = tx.SettleSuccess()
	if err != nil {
		t.Fatal(err)
	}
	change = nil
	for index := range writes {
		if writes[index].Type == "document.change" {
			change = &writes[index]
		}
	}
	if change == nil || change.DocumentContent.Kind != ContentBase {
		t.Fatalf("checkpoint change = %+v", change)
	}
}

func TestTransactionRetiresDocument(t *testing.T) {
	storage := NewMemoryStorage()
	host := newTestTransactionHost(storage)
	definition := sessionDocDefinition()
	setup := NewTransaction(host, context.Background(), TransactionScope{})
	if _, err := setup.CreateRootConversation(); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.Doc(definition); err != nil {
		t.Fatal(err)
	}
	mustSettle(t, setup, host)

	tx := NewTransaction(host, context.Background(), TransactionScope{})
	if err := tx.RetireDoc(definition); err != nil {
		t.Fatal(err)
	}
	writes, err := tx.SettleSuccess()
	if err != nil {
		t.Fatal(err)
	}
	var retire bool
	for _, write := range writes {
		if write.Type == "document.retire" {
			retire = true
		}
	}
	if !retire {
		t.Fatalf("writes = %+v", writes)
	}
	seq, err := storage.Commit(context.Background(), writes)
	if err != nil {
		t.Fatal(err)
	}
	publications := tx.Adopt(seq)
	if len(publications) != 1 || publications[0].Value != nil || publications[0].Record.RetiredAt == nil {
		t.Fatalf("publications = %+v", publications)
	}
	address := DocumentAddress{Kind: "notes", Scope: DocumentScope{Kind: ScopeSession}}
	if host.Cached(AddressID(address)) != nil {
		t.Fatal("retired incarnation stayed cached")
	}
}

func TestTransactionForkCopiesDocumentsAndRejectsSourceWrites(t *testing.T) {
	storage := NewMemoryStorage()
	host := newTestTransactionHost(storage)
	root := RootConversationID
	rootScope := DocumentScope{Kind: ScopeConversation, ConversationID: &root}
	definition := DocDefinition{
		Kind: "notes", Version: 1, Scope: ScopeConversation, History: strPtr(HistoryRewindable), Fork: strPtr(ForkAsOf),
		Initial: func(seed chord.JsonValue) (chord.JsonValue, error) { return map[string]any{}, nil },
	}
	setup := NewTransaction(host, context.Background(), TransactionScope{})
	if _, err := setup.CreateRootConversation(); err != nil {
		t.Fatal(err)
	}
	draft, err := setup.Doc(definition, root)
	if err != nil {
		t.Fatal(err)
	}
	draft.(map[string]any)["name"] = "origin"
	entry, err := setup.AppendEntry(root, EntryDraft{Kind: "message"})
	if err != nil {
		t.Fatal(err)
	}
	setupWrites, err := setup.SettleSuccess()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Commit(context.Background(), setupWrites); err != nil {
		t.Fatal(err)
	}

	// The fork copies the as-of document and forbids changing the source.
	tx := NewTransaction(host, context.Background(), TransactionScope{})
	forked, err := tx.ForkConversation(root, entry.ID, ConversationOwnership{Kind: ConversationOwnerless})
	if err != nil {
		t.Fatal(err)
	}
	if forked.Parent == nil || forked.Parent.At != entry.ID {
		t.Fatalf("forked = %+v", forked)
	}
	var copyWrite bool
	writes, err := tx.SettleSuccess()
	if err != nil {
		t.Fatal(err)
	}
	for _, write := range writes {
		if write.Type == "document.copy" {
			copyWrite = true
			if write.DocumentCreate.Scope.ConversationID == nil || *write.DocumentCreate.Scope.ConversationID != forked.ID {
				t.Fatalf("copy = %+v", write.DocumentCreate)
			}
		}
	}
	if !copyWrite {
		t.Fatalf("writes = %+v", writes)
	}

	// Changing a copied source document in the fork transaction is rejected.
	source, err := storage.FindDocument(context.Background(), DocumentAddress{Kind: "notes", Scope: rootScope}, CurrentDocumentPoint())
	if err != nil || source == nil {
		t.Fatalf("source = %+v, %v", source, err)
	}
	host.docs = map[string]*LoadedDocument{}
	tx = NewTransaction(host, context.Background(), TransactionScope{})
	if _, err := tx.ForkConversation(root, entry.ID, ConversationOwnership{Kind: ConversationOwnerless}); err != nil {
		t.Fatal(err)
	}
	draft, err = tx.Doc(definition, root)
	if err != nil {
		t.Fatal(err)
	}
	draft.(map[string]any)["name"] = "changed"
	if _, err := tx.SettleSuccess(); err == nil ||
		!strings.Contains(err.Error(), "Cannot change fork source document") {
		t.Fatalf("fork source error = %v", err)
	}
}

func TestTransactionSubmissionLifecycle(t *testing.T) {
	storage := NewMemoryStorage()
	host := newTestTransactionHost(storage)
	requestID := "req-1"
	tx := NewTransaction(host, context.Background(), TransactionScope{})
	if _, err := tx.CreateRootConversation(); err != nil {
		t.Fatal(err)
	}
	submission, err := tx.CreateSubmission(SubmissionCreate{
		ConversationID: RootConversationID, Type: SubmissionTypeInput, RequestID: &requestID, Status: SubmissionQueued,
	})
	if err != nil {
		t.Fatal(err)
	}
	if submission.Type != SubmissionTypeInput {
		t.Fatalf("submission = %+v", submission)
	}
	// Settling a queued input as done is rejected: only a placed input can be
	// answered.
	if err := tx.SettleSubmission(submission.ID, SubmissionSettlement{Status: SubmissionDone, Answer: idPtr(2)}); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.SettleSuccess(); err == nil || !strings.Contains(err.Error(), "not a placed input") {
		t.Fatalf("settle = %v", err)
	}

	// Place it, then settle with an answer.
	storage2 := NewMemoryStorage()
	host2 := newTestTransactionHost(storage2)
	tx = NewTransaction(host2, context.Background(), TransactionScope{})
	if _, err := tx.CreateRootConversation(); err != nil {
		t.Fatal(err)
	}
	entry, err := tx.AppendEntry(RootConversationID, EntryDraft{Kind: "message"})
	if err != nil {
		t.Fatal(err)
	}
	submission, err = tx.CreateSubmission(SubmissionCreate{
		ConversationID: RootConversationID, Type: SubmissionTypeInput, RequestID: &requestID, Status: SubmissionQueued,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.PlaceSubmission(submission.ID, entry.ID); err != nil {
		t.Fatal(err)
	}
	writes, err := tx.SettleSuccess()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage2.Commit(context.Background(), writes); err != nil {
		t.Fatal(err)
	}
	stored, err := storage2.Submission(context.Background(), submission.ID)
	if err != nil || stored == nil || stored.Status != SubmissionPlaced || stored.Entry == nil || *stored.Entry != entry.ID {
		t.Fatalf("stored = %+v, %v", stored, err)
	}

	// A placed input settles to done with its answer.
	tx = NewTransaction(host2, context.Background(), TransactionScope{})
	if err := tx.SettleSubmission(submission.ID, SubmissionSettlement{Status: SubmissionDone, Answer: idPtr(entry.ID)}); err != nil {
		t.Fatal(err)
	}
	writes, err = tx.SettleSuccess()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage2.Commit(context.Background(), writes); err != nil {
		t.Fatal(err)
	}
	stored, _ = storage2.Submission(context.Background(), submission.ID)
	if stored.Status != SubmissionDone || stored.Answer == nil || *stored.Answer != entry.ID {
		t.Fatalf("settled = %+v", stored)
	}
}

func TestTransactionTerminalTaskRetiresItsDocuments(t *testing.T) {
	storage := NewMemoryStorage()
	host := newTestTransactionHost(storage)
	setup := NewTransaction(host, context.Background(), TransactionScope{})
	if _, err := setup.CreateRootConversation(); err != nil {
		t.Fatal(err)
	}
	taskID, err := setup.CreateTask(TaskDefinition{
		Name: "t", Version: 1,
		Initial: func(input json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`{"phase":"start"}`), nil },
	}, json.RawMessage(`{}`), TaskOptions{
		Ownership: TaskOwnership{Kind: TaskOwnedByConversation}, ConversationID: idPtr(RootConversationID),
	})
	if err != nil {
		t.Fatal(err)
	}
	taskScope := DocumentScope{Kind: ScopeTask, TaskID: &taskID}
	definition := DocDefinition{
		Kind: "scratch", Version: 1, Scope: ScopeTask,
		Initial: func(seed chord.JsonValue) (chord.JsonValue, error) { return map[string]any{}, nil },
	}
	if _, err := setup.Doc(definition, taskID); err != nil {
		t.Fatal(err)
	}
	mustSettle(t, setup, host)

	// Finishing the task retires its documents in the same commit.
	tx := NewTransaction(host, context.Background(), TransactionScope{})
	task, err := tx.Task(taskID)
	if err != nil || task == nil {
		t.Fatalf("task = %+v, %v", task, err)
	}
	finished := *task
	finished.State = TaskState{Status: TaskTerminal, Outcome: &TaskOutcome{Status: OutcomeCompleted, Result: json.RawMessage(`1`)}}
	if err := tx.SetTask(&finished); err != nil {
		t.Fatal(err)
	}
	writes, err := tx.SettleSuccess()
	if err != nil {
		t.Fatal(err)
	}
	var retired bool
	for _, write := range writes {
		if write.Type == "document.retire" {
			retired = true
		}
	}
	if !retired {
		t.Fatalf("writes = %+v", writes)
	}
	if _, err := storage.Commit(context.Background(), writes); err != nil {
		t.Fatal(err)
	}
	record, err := storage.FindDocument(context.Background(), DocumentAddress{Kind: "scratch", Scope: taskScope}, CurrentDocumentPoint())
	if err != nil {
		t.Fatal(err)
	}
	if record != nil {
		t.Fatalf("document stayed alive: %+v", record)
	}
}
