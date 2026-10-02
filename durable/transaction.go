package durable

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
)

// Port of session/transaction.ts: the transaction for one session commit
// callback, over the port's synchronous storage.
//
// The reference tracks every asynchronous operation so callback settlement can
// reject and drain unfinished work (Promises). The port's storage is
// synchronous, so the transaction itself is synchronous: a failed step returns
// an error and the caller settles the transaction.

const internalScanPageSize = 256

// TransactionScope are the defaults a commit binds to (upstream
// TransactionScope).
type TransactionScope struct {
	// ConversationID is the conversation Tx.CreateTask() defaults to.
	ConversationID *Id
	// TaskID is the task whose runtime commit this is; stamped as ByTaskID on
	// appended entries.
	TaskID *Id
}

// Transaction is one session commit callback (upstream Transaction).
type Transaction struct {
	host  TransactionHost
	ctx   context.Context
	scope TransactionScope

	sealed        bool
	hasTableWrite bool

	writes                    []StorageWrite
	createdConversationIDs    map[Id]struct{}
	forkSourceConversationIDs map[Id]struct{}
	forkSourceDocumentIDs     map[Id]struct{}

	tasksByID map[Id]*transactionTask
	// submissions created by this transaction, by ID.
	submissions map[Id]*SubmissionRecord
	// submissionChanges are settlements and placements in staging order.
	submissionChanges []submissionChange

	plans                   []*documentPlan
	documents               []*documentEntry
	latestDocumentByAddress map[string]*documentEntry
}

type transactionTask struct {
	committedRead             *TaskRecord
	committedLoaded           bool
	writeKind                 string // "create" | "replace"
	write                     *TaskRecord
	publicationConversationID *Id
}

type submissionChange struct {
	id         Id
	settlement *SubmissionSettlement
	placement  *Id
}

// documentEntry is one document incarnation acquired, created or retired by
// this transaction (upstream DocumentEntry).
type documentEntry struct {
	addressID  string
	address    DocumentAddress
	definition *DocDefinition
	// draft is the public mutable acquisition; nil for a metadata-only
	// retirement.
	draft          chord.JsonValue
	target         *documentTarget
	change         *delta.Change
	prepared       *delta.Prepared
	retireOnCommit bool
}

type documentTarget struct {
	kind string // loaded | created | fork-copy | retire-only
	// loaded
	document *LoadedDocument
	// created / fork-copy
	record  *DocumentCreate
	source  *DocumentCopySource
	version int
	tracker *delta.Tracker
	// retire-only
	retireRecord *DocumentRecord
}

// documentPlan is what one staged incarnation writes and publishes (upstream
// DocumentPlan).
type documentPlan struct {
	addressID string
	// create is set for a new incarnation, existing for a committed one.
	create         *DocumentCreate
	existing       *DocumentRecord
	retire         bool
	content        *StorageWrite
	change         *planChange
	conversationID *Id
}

type planChange struct {
	tracker    *delta.Tracker
	prepared   *delta.Prepared
	version    int
	loaded     *LoadedDocument
	definition *DocDefinition
}

// NewTransaction starts a transaction over one host and context.
func NewTransaction(host TransactionHost, ctx context.Context, scope TransactionScope) *Transaction {
	return &Transaction{
		host:                      host,
		ctx:                       ctx,
		scope:                     scope,
		createdConversationIDs:    map[Id]struct{}{},
		forkSourceConversationIDs: map[Id]struct{}{},
		forkSourceDocumentIDs:     map[Id]struct{}{},
		tasksByID:                 map[Id]*transactionTask{},
		submissions:               map[Id]*SubmissionRecord{},
		latestDocumentByAddress:   map[string]*documentEntry{},
	}
}

// ─── Table reads ────────────────────────────────────────────────────────────

// Conversation reads one conversation.
func (tx *Transaction) Conversation(id Id) (*ConversationRecord, error) {
	var result *ConversationRecord
	if err := tx.read("conversation", func() error {
		record, err := tx.host.Storage().Conversation(tx.ctx, id)
		result = record
		return err
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// Entry reads one entry.
func (tx *Transaction) Entry(id Id) (*EntryRecord, error) {
	var result *EntryRecord
	if err := tx.read("entry", func() error {
		commit, err := tx.host.Storage().Entry(tx.ctx, id)
		if commit != nil {
			result = &commit.Entry
		}
		return err
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// EntryOf reads one entry of an expected kind (upstream's typed entry
// overload).
func (tx *Transaction) EntryOf(token Entry, id Id) (*EntryRecord, error) {
	record, err := tx.Entry(id)
	if err != nil {
		return nil, err
	}
	if !token.Matches(record) {
		return nil, nil
	}
	return record, nil
}

// Task reads the committed record of one task.
func (tx *Transaction) Task(id Id) (*TaskRecord, error) {
	var result *TaskRecord
	if err := tx.read("task", func() error {
		record, err := tx.committedTask(id)
		result = record
		return err
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// ScanConversations scans conversations in ascending id order.
func (tx *Transaction) ScanConversations(query ConversationQuery, limit int, cursor Cursor) (Page[ConversationRecord], error) {
	var result Page[ConversationRecord]
	if err := tx.read("scanConversations", func() error {
		page, err := tx.host.Storage().ScanConversations(tx.ctx, query, cursor, limit)
		result = page
		return err
	}); err != nil {
		return Page[ConversationRecord]{}, err
	}
	return result, nil
}

// ScanEntries scans the visible entry range newest-first.
func (tx *Transaction) ScanEntries(query EntryQuery, limit int, cursor Cursor) (Page[EntryRecord], error) {
	var result Page[EntryRecord]
	if err := tx.read("scanEntries", func() error {
		page, err := tx.host.Storage().ScanEntries(tx.ctx, query, cursor, limit)
		result = page
		return err
	}); err != nil {
		return Page[EntryRecord]{}, err
	}
	return result, nil
}

// LatestHeadMarker returns the newest visible head marker.
func (tx *Transaction) LatestHeadMarker(conversationID Id) (*EntryRecord, error) {
	var result *EntryRecord
	if err := tx.read("latestHeadMarker", func() error {
		record, err := tx.host.Storage().FindLatestHeadMarker(tx.ctx, conversationID, nil)
		result = record
		return err
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// ScanTasks scans task records matching every supplied filter.
func (tx *Transaction) ScanTasks(query TaskQuery, limit int, cursor Cursor) (Page[TaskRecord], error) {
	var result Page[TaskRecord]
	if err := tx.read("scanTasks", func() error {
		page, err := tx.host.Storage().ScanTasks(tx.ctx, query, cursor, limit)
		result = page
		return err
	}); err != nil {
		return Page[TaskRecord]{}, err
	}
	return result, nil
}

// Submission reads one committed submission record.
func (tx *Transaction) Submission(id Id) (*SubmissionRecord, error) {
	var result *SubmissionRecord
	if err := tx.read("submission", func() error {
		record, err := tx.host.Storage().Submission(tx.ctx, id)
		result = record
		return err
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// SubmissionByRequest reads a committed submission by its conversation-scoped
// request key.
func (tx *Transaction) SubmissionByRequest(conversationID Id, requestID string) (*SubmissionRecord, error) {
	var result *SubmissionRecord
	if err := tx.read("submissionByRequest", func() error {
		record, err := tx.host.Storage().SubmissionByRequest(tx.ctx, conversationID, requestID)
		result = record
		return err
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// ─── Table writes ───────────────────────────────────────────────────────────

// CreateConversation stages a new conversation with explicit ownership.
func (tx *Transaction) CreateConversation(ownership ConversationOwnership) (*ConversationRecord, error) {
	return tx.stageConversation(nil, ownership, nil)
}

// CreateRootConversation stages the reserved root identity.
func (tx *Transaction) CreateRootConversation() (*ConversationRecord, error) {
	reserved := RootConversationID
	return tx.stageConversation(nil, ConversationOwnership{Kind: ConversationOwnerless}, &reserved)
}

// ForkConversation stages a conversation forked from a parent at an entry.
func (tx *Transaction) ForkConversation(parentConversationID Id, at Id, ownership ConversationOwnership) (*ConversationRecord, error) {
	return tx.stageConversation(&ConversationParent{ConversationID: parentConversationID, At: at}, ownership, nil)
}

func (tx *Transaction) stageConversation(parent *ConversationParent, ownership ConversationOwnership, reservedID *Id) (*ConversationRecord, error) {
	if err := tx.write(func() error { return nil }); err != nil {
		return nil, err
	}
	ownerTaskID := ownership.TaskID
	id := Id(0)
	if reservedID != nil {
		id = *reservedID
	} else {
		minted, err := tx.host.Storage().MintID(tx.ctx)
		if err != nil {
			return nil, err
		}
		id = minted
	}
	if err := tx.assertOpen(); err != nil {
		return nil, err
	}
	var owner *ConversationOwner
	if ownerTaskID != nil {
		task, err := tx.currentTask(*ownerTaskID)
		if err != nil {
			return nil, err
		}
		if task == nil {
			return nil, fmt.Errorf("Conversation owner task %d does not exist", *ownerTaskID)
		}
		owner = &ConversationOwner{ConversationID: task.ConversationID, TaskID: *ownerTaskID}
	}
	record := ConversationRecord{ID: id, Parent: parent, Owner: owner}
	if parent != nil {
		copies, err := PrepareForkDocumentCopies(tx.ctx, tx.host.Storage(), parent.ConversationID, parent.At, id)
		if err != nil {
			return nil, err
		}
		for _, copy := range copies {
			tx.forkSourceDocumentIDs[copy.Source.ID] = struct{}{}
			entry := &documentEntry{
				addressID: AddressID(DocumentAddress{Kind: copy.Record.Kind, Scope: copy.Record.Scope, Key: copy.Record.Key}),
				address:   DocumentAddress{Kind: copy.Record.Kind, Scope: copy.Record.Scope, Key: copy.Record.Key},
				target:    &documentTarget{kind: "fork-copy", record: copy.Record, source: &copy.Source},
			}
			tx.documents = append(tx.documents, entry)
			tx.latestDocumentByAddress[entry.addressID] = entry
		}
		tx.forkSourceConversationIDs[parent.ConversationID] = struct{}{}
	}
	tx.createdConversationIDs[id] = struct{}{}
	tx.writes = append(tx.writes, StorageWrite{Type: "conversation", Conversation: &record})
	if err := tx.host.ConversationCreated(tx, record); err != nil {
		return nil, err
	}
	if err := tx.assertOpen(); err != nil {
		return nil, err
	}
	return &record, nil
}

// AppendEntry stages one entry in a conversation. The transaction stamps the
// id, conversation, head and task attribution.
func (tx *Transaction) AppendEntry(conversationID Id, draft EntryDraft) (*EntryRecord, error) {
	var result *EntryRecord
	if err := tx.write(func() error {
		if err := tx.requireConversation(conversationID); err != nil {
			return err
		}
		if err := tx.assertOpen(); err != nil {
			return err
		}
		id, err := tx.host.Storage().MintID(tx.ctx)
		if err != nil {
			return err
		}
		record := EntryRecord{
			ID:             id,
			ConversationID: conversationID,
			Kind:           draft.Kind,
			Model:          draft.Model,
			Data:           draft.Data,
			Head:           draft.Head,
			Edits:          draft.Edits,
			ByTaskID:       tx.scope.TaskID,
		}
		if draft.HeadSelf {
			self := id
			record.Head = &self
		}
		tx.writes = append(tx.writes, StorageWrite{Type: "entry", Entry: &record})
		result = &record
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// CreateTask stages one new task and returns its id.
func (tx *Transaction) CreateTask(definition TaskDefinition, input json.RawMessage, options TaskOptions) (Id, error) {
	var result Id
	if err := tx.write(func() error {
		var owner *TaskRecord
		if options.Ownership.Kind == TaskOwnedByTask {
			if options.Ownership.TaskID == nil {
				return fmt.Errorf("Task ownership requires a task ID")
			}
			current, err := tx.currentTask(*options.Ownership.TaskID)
			if err != nil {
				return err
			}
			if current == nil {
				return fmt.Errorf("Task owner %d does not exist", *options.Ownership.TaskID)
			}
			if options.Background {
				return fmt.Errorf("A child task cannot be background")
			}
			if options.ConversationID != nil && *options.ConversationID != current.ConversationID {
				return fmt.Errorf("A child task lives in its owner's conversation %d", current.ConversationID)
			}
			owner = current
		}
		conversationID := tx.scope.ConversationID
		if owner != nil {
			conversationID = &owner.ConversationID
		} else if options.ConversationID != nil {
			conversationID = options.ConversationID
		}
		if conversationID == nil {
			return fmt.Errorf("Tx.CreateTask() requires options.conversationId")
		}
		if err := tx.requireConversation(*conversationID); err != nil {
			return err
		}
		if err := tx.assertOpen(); err != nil {
			return err
		}
		checkpoint, err := definition.Initial(input)
		if err != nil {
			return err
		}
		id, err := tx.host.Storage().MintID(tx.ctx)
		if err != nil {
			return err
		}
		record := &TaskRecord{
			ID:             id,
			ConversationID: *conversationID,
			Kind:           definition.Name,
			Version:        definition.Version,
			Input:          input,
			Background:     options.Background,
			AbortRequested: false,
			State:          TaskState{Status: TaskPending, Checkpoint: checkpoint},
		}
		if owner != nil {
			ownerID := owner.ID
			record.Owner = &ownerID
		}
		tx.tasksByID[id] = &transactionTask{writeKind: "create", write: record}
		result = id
		return nil
	}); err != nil {
		return 0, err
	}
	return result, nil
}

// CreateSubmission stages a raw submission record with a fresh id.
func (tx *Transaction) CreateSubmission(create SubmissionCreate) (*SubmissionRecord, error) {
	var result *SubmissionRecord
	if err := tx.write(func() error {
		if err := tx.requireConversation(create.ConversationID); err != nil {
			return err
		}
		if err := tx.assertOpen(); err != nil {
			return err
		}
		id, err := tx.host.Storage().MintID(tx.ctx)
		if err != nil {
			return err
		}
		record := &SubmissionRecord{
			ID:             id,
			ConversationID: create.ConversationID,
			Type:           create.Type,
			RequestID:      create.RequestID,
			Status:         create.Status,
			Entry:          create.Entry,
			Answer:         create.Answer,
			Reason:         create.Reason,
			Detail:         create.Detail,
		}
		tx.submissions[id] = record
		result = record
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// SettleSubmission stages a submission settlement, resolved during assembly
// against the latest candidate record.
func (tx *Transaction) SettleSubmission(id Id, settlement SubmissionSettlement) error {
	if err := tx.assertOpen(); err != nil {
		return err
	}
	tx.hasTableWrite = true
	copied := settlement
	tx.submissionChanges = append(tx.submissionChanges, submissionChange{id: id, settlement: &copied})
	return nil
}

// PlaceSubmission stages the placement of a queued submission at an entry.
func (tx *Transaction) PlaceSubmission(id Id, entry Id) error {
	if err := tx.assertOpen(); err != nil {
		return err
	}
	tx.hasTableWrite = true
	tx.submissionChanges = append(tx.submissionChanges, submissionChange{id: id, placement: &entry})
	return nil
}

// SetTask replaces one task record completely.
func (tx *Transaction) SetTask(value *TaskRecord) error {
	if err := tx.assertOpen(); err != nil {
		return err
	}
	tx.hasTableWrite = true
	task := tx.taskEntry(value.ID)
	if task.write != nil && task.write.State.Status == TaskTerminal {
		return fmt.Errorf("Task %d already has a terminal candidate", value.ID)
	}
	if task.write != nil && task.write.ConversationID != value.ConversationID {
		return fmt.Errorf("Task %d cannot change conversations", value.ID)
	}
	kind := "replace"
	if task.writeKind == "create" {
		kind = "create"
	}
	task.writeKind = kind
	// The caller keeps ownership of the record it passed; stage a detached copy
	// so later mutations cannot alias the candidate (upstream copyJson).
	task.write = cloneTask(value)
	return nil
}

// StagedTasks returns the candidate records this transaction created or
// replaced.
func (tx *Transaction) StagedTasks() []*TaskRecord {
	var records []*TaskRecord
	for _, task := range tx.tasksByID {
		if task.write != nil {
			records = append(records, task.write)
		}
	}
	return records
}

// StagedConversations returns the conversations this transaction created or
// forked.
func (tx *Transaction) StagedConversations() []ConversationRecord {
	var records []ConversationRecord
	for _, write := range tx.writes {
		if write.Type == "conversation" && write.Conversation != nil {
			records = append(records, *write.Conversation)
		}
	}
	return records
}

// ─── Documents ──────────────────────────────────────────────────────────────

// Doc acquires a mutable draft at a document address. Args are the owner id
// (conversation or task scope) and the family key, as the definition requires.
func (tx *Transaction) Doc(definition DocDefinition, args ...any) (chord.JsonValue, error) {
	if err := tx.assertOpen(); err != nil {
		return nil, err
	}
	resolved, err := ResolveAddress(definition, args...)
	if err != nil {
		return nil, err
	}
	if err := tx.assertTaskDocumentsOpen(resolved); err != nil {
		return nil, err
	}
	latest := tx.latestDocumentByAddress[resolved.ID]
	if latest != nil && !latest.retireOnCommit {
		if latest.draft != nil {
			return latest.draft, nil
		}
		if latest.target != nil && latest.target.kind == "fork-copy" {
			draft, err := tx.acquireForkCopy(latest, definition, latest.target)
			if err != nil {
				return nil, err
			}
			latest.draft = draft
			return draft, nil
		}
	}
	var seed chord.JsonValue
	if definition.Family {
		if resolved.NextArgument >= len(args) {
			return nil, fmt.Errorf("Document %s requires a family seed", definition.Kind)
		}
		seed = chord.CloneJSON(args[resolved.NextArgument])
	}
	entry := &documentEntry{addressID: resolved.ID, address: resolved.Address, definition: &definition}
	tx.documents = append(tx.documents, entry)
	tx.latestDocumentByAddress[entry.addressID] = entry
	skipLoad := latest != nil && latest.retireOnCommit
	draft, err := tx.acquire(entry, seed, skipLoad)
	if err != nil {
		return nil, err
	}
	entry.draft = draft
	return draft, nil
}

// RetireDoc stages the retirement of the current incarnation at an address.
func (tx *Transaction) RetireDoc(definition DocDefinition, args ...any) error {
	if err := tx.assertOpen(); err != nil {
		return err
	}
	resolved, err := ResolveAddress(definition, args...)
	if err != nil {
		return err
	}
	latest := tx.latestDocumentByAddress[resolved.ID]
	if latest != nil && latest.retireOnCommit {
		return nil
	}
	if latest != nil && latest.target != nil && latest.target.kind == "fork-copy" {
		if err := CheckRecordScope(definition, SemanticsOfCreate(latest.target.record)); err != nil {
			return err
		}
		latest.retireOnCommit = true
		return nil
	}
	if latest != nil && latest.draft != nil {
		// Retirement of an acquired draft persists its final content first.
		latest.retireOnCommit = true
		return nil
	}
	entry := &documentEntry{addressID: resolved.ID, address: resolved.Address, definition: &definition, retireOnCommit: true}
	tx.documents = append(tx.documents, entry)
	tx.latestDocumentByAddress[entry.addressID] = entry
	return tx.findRetirement(entry)
}

func (tx *Transaction) acquire(entry *documentEntry, seed chord.JsonValue, skipLoad bool) (chord.JsonValue, error) {
	definition := *entry.definition
	var loaded *LoadedDocument
	if !skipLoad {
		var err error
		loaded, err = tx.host.Load(tx.ctx, definition, entry.addressID, entry.address)
		if err != nil {
			return nil, err
		}
	}
	if err := tx.assertOpen(); err != nil {
		return nil, err
	}
	if loaded != nil {
		if err := CheckRecordScope(definition, SemanticsOfRecord(&loaded.Record)); err != nil {
			return nil, err
		}
		if err := CheckRecordVersion(definition, SemanticsOfRecord(&loaded.Record), loaded.StoredVersion); err != nil {
			return nil, err
		}
		entry.target = &documentTarget{kind: "loaded", document: loaded}
		entry.change = loaded.Tracker.BeginChange()
		return entry.change.State(), nil
	}
	scope := entry.address.Scope
	if scope.Kind == ScopeConversation && scope.ConversationID != nil {
		if err := tx.requireConversation(*scope.ConversationID); err != nil {
			return nil, err
		}
	}
	if scope.Kind == ScopeTask && scope.TaskID != nil {
		task, err := tx.currentTask(*scope.TaskID)
		if err != nil {
			return nil, err
		}
		if task == nil {
			return nil, fmt.Errorf("Task %d does not exist", *scope.TaskID)
		}
		if task.State.Status == TaskTerminal {
			return nil, fmt.Errorf("Task %d is terminal", *scope.TaskID)
		}
	}
	if err := tx.assertOpen(); err != nil {
		return nil, err
	}
	var value chord.JsonValue
	var err error
	if definition.Family {
		value, err = definition.Initial(seed)
	} else {
		value, err = definition.Initial(nil)
	}
	if err != nil {
		return nil, err
	}
	value = chord.CloneJSON(value)
	id, err := tx.host.Storage().MintID(tx.ctx)
	if err != nil {
		return nil, err
	}
	tracker := delta.Track(value)
	entry.target = &documentTarget{
		kind:    "created",
		record:  DocumentCreateFor(definition, entry.address, id),
		version: definition.Version,
		tracker: tracker,
	}
	entry.change = tracker.BeginChange()
	return entry.change.State(), nil
}

func (tx *Transaction) acquireForkCopy(entry *documentEntry, definition DocDefinition, target *documentTarget) (chord.JsonValue, error) {
	stored, err := tx.host.Storage().Document(tx.ctx, target.source.ID, target.source.At)
	if err != nil {
		return nil, err
	}
	if err := tx.assertOpen(); err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, fmt.Errorf("Fork source document %d cannot be read", target.source.ID)
	}
	record := target.record
	scope := stored.Record.Scope
	if scope.Kind != ScopeConversation || stored.Record.Kind != record.Kind ||
		!sameOptionalString(stored.Record.Key, record.Key) ||
		!sameOptionalString(stored.Record.History, record.History) ||
		!sameOptionalString(stored.Record.Fork, record.Fork) {
		return nil, fmt.Errorf("Fork source document %d does not match the copied record", target.source.ID)
	}
	value, err := MaterializeDocumentValue(definition, SemanticsOfCreate(record), stored.Version, stored.Value)
	if err != nil {
		return nil, err
	}
	tracker := delta.Track(value)
	entry.definition = &definition
	entry.target = &documentTarget{kind: "created", record: record, version: definition.Version, tracker: tracker}
	entry.change = tracker.BeginChange()
	return entry.change.State(), nil
}

func (tx *Transaction) findRetirement(entry *documentEntry) error {
	var record *DocumentRecord
	if cached := tx.host.Cached(entry.addressID); cached != nil {
		copied := cached.Record
		record = &copied
	} else {
		found, err := tx.host.Storage().FindDocument(tx.ctx, entry.address, CurrentDocumentPoint())
		if err != nil {
			return err
		}
		record = found
	}
	if err := tx.assertOpen(); err != nil {
		return err
	}
	if record == nil {
		return nil
	}
	if err := CheckRecordScope(*entry.definition, SemanticsOfRecord(record)); err != nil {
		return err
	}
	entry.target = &documentTarget{kind: "retire-only", retireRecord: record}
	return nil
}

// ─── Settlement ─────────────────────────────────────────────────────────────

// SettleFailure seals the transaction after a callback failure and aborts
// every change.
func (tx *Transaction) SettleFailure() {
	tx.sealed = true
	tx.abortChanges()
}

// SettleSuccess seals the transaction, prepares every open change and
// assembles the atomic batch.
func (tx *Transaction) SettleSuccess() ([]StorageWrite, error) {
	tx.sealed = true
	for _, document := range tx.documents {
		if document.change != nil {
			prepared, err := document.change.Prepare()
			if err != nil {
				tx.abortChanges()
				return nil, err
			}
			document.prepared = prepared
		}
	}
	writes, err := tx.assemble()
	if err != nil {
		tx.abortChanges()
		return nil, err
	}
	return writes, nil
}

// Discard aborts every prepared change.
func (tx *Transaction) Discard() { tx.abortChanges() }

// Adopt adopts every prepared change after storage success and describes the
// publication.
func (tx *Transaction) Adopt(seq Seq) []DocumentCommitChange {
	var publications []DocumentCommitChange
	for _, plan := range tx.plans {
		committed := plan.existing != nil
		var record DocumentRecord
		if committed {
			record = *plan.existing
		} else {
			record = DocumentRecord{
				ID: plan.create.ID, Kind: plan.create.Kind, Key: plan.create.Key, Scope: plan.create.Scope,
				History: plan.create.History, Fork: plan.create.Fork, CreatedAt: seq,
			}
		}
		if plan.retire {
			retired := seq
			record.RetiredAt = &retired
		}
		change := plan.change
		if change != nil {
			if change.loaded == nil {
				if !plan.retire {
					if err := change.tracker.Adopt(change.prepared); err != nil {
						panic(err)
					}
				} else {
					change.prepared.Abort()
				}
			} else if len(change.prepared.Ops()) > 0 {
				if err := change.tracker.Adopt(change.prepared); err != nil {
					panic(err)
				}
			} else {
				change.prepared.Abort()
			}
			if change.loaded != nil {
				if change.loaded.StoredVersion < change.version {
					change.loaded.StoredVersion = change.version
				}
				if plan.content != nil && plan.content.Type == "document.change" {
					if plan.content.DocumentContent.Kind == ContentBase {
						change.loaded.DeltasSinceBase = 0
					} else {
						change.loaded.DeltasSinceBase++
					}
				}
			} else if !plan.retire {
				tx.host.Install(LoadedDocument{
					AddressID:       plan.addressID,
					Record:          record,
					StoredVersion:   change.version,
					ValueVersion:    change.version,
					DeltasSinceBase: 0,
					Tracker:         change.tracker,
				})
			}
		}
		if plan.retire {
			if committed {
				tx.host.Evict(plan.addressID, record.ID)
			}
			publications = append(publications, DocumentCommitChange{
				Type: "document", Record: record, ConversationID: plan.conversationID, Ops: []delta.Op{},
			})
		} else if plan.content != nil && plan.content.Type == "document.copy" {
			publications = append(publications, DocumentCommitChange{
				Type: "document.copy", Record: record, ConversationID: plan.conversationID,
				Source: plan.content.DocumentSource,
			})
		} else if change != nil && publishes(plan) {
			var ops []delta.Op
			if change.loaded != nil {
				ops = change.prepared.Ops()
			}
			version := change.version
			publications = append(publications, DocumentCommitChange{
				Type: "document", Record: record, ConversationID: plan.conversationID,
				Version: &version, Value: change.prepared.Value(), Ops: ops,
			})
		}
	}
	return publications
}

func (tx *Transaction) assemble() ([]StorageWrite, error) {
	storage := tx.host.Storage()
	for _, document := range tx.documents {
		plan := planDocument(document)
		if plan != nil {
			tx.plans = append(tx.plans, plan)
		}
	}
	if err := tx.rejectForkSourceWrites(); err != nil {
		return nil, err
	}
	if err := tx.validateOwners(); err != nil {
		return nil, err
	}
	for id, task := range tx.tasksByID {
		if task.writeKind != "replace" {
			continue
		}
		committed, err := tx.committedTask(id)
		if err != nil {
			return nil, err
		}
		if committed == nil {
			return nil, fmt.Errorf("Task %d does not exist", id)
		}
		if committed.State.Status == TaskTerminal {
			return nil, fmt.Errorf("Task %d is already terminal", id)
		}
		if committed.ConversationID != task.write.ConversationID {
			return nil, fmt.Errorf("Task %d cannot change conversations", id)
		}
	}

	terminalTaskIDs := map[Id]struct{}{}
	for _, task := range tx.tasksByID {
		if task.write != nil && task.write.State.Status == TaskTerminal {
			terminalTaskIDs[task.write.ID] = struct{}{}
		}
	}
	if len(terminalTaskIDs) > 0 {
		retiring := map[Id]struct{}{}
		for _, plan := range tx.plans {
			scope := planRecordScope(plan)
			if scope == nil || scope.Kind != ScopeTask || scope.TaskID == nil {
				continue
			}
			if _, terminal := terminalTaskIDs[*scope.TaskID]; !terminal {
				continue
			}
			plan.retire = true
			retiring[planRecordID(plan)] = struct{}{}
		}
		for taskID := range terminalTaskIDs {
			if task := tx.tasksByID[taskID]; task != nil && task.writeKind == "create" {
				continue
			}
			var cursor Cursor
			for {
				page, err := storage.ScanDocuments(tx.ctx,
					DocumentQuery{Scope: DocumentScope{Kind: ScopeTask, TaskID: &taskID}, At: CurrentDocumentPoint()},
					cursor, internalScanPageSize)
				if err != nil {
					return nil, err
				}
				for index := range page.Items {
					record := page.Items[index]
					if _, done := retiring[record.ID]; done {
						continue
					}
					plan := &documentPlan{addressID: AddressID(DocumentAddress{Kind: record.Kind, Scope: record.Scope, Key: record.Key}), existing: &record, retire: true}
					tx.plans = append(tx.plans, plan)
					retiring[record.ID] = struct{}{}
				}
				cursor = page.Next
				if cursor == nil {
					break
				}
			}
		}
	}

	// Resolve publication ownership before storage admission.
	for _, plan := range tx.plans {
		if !publishes(plan) {
			continue
		}
		scope := planRecordScope(plan)
		if scope == nil {
			continue
		}
		if scope.Kind == ScopeConversation {
			plan.conversationID = scope.ConversationID
		}
		if scope.Kind != ScopeTask || scope.TaskID == nil {
			continue
		}
		task := tx.taskEntry(*scope.TaskID)
		if task.publicationConversationID == nil {
			current, err := tx.currentTask(*scope.TaskID)
			if err != nil {
				return nil, err
			}
			if current != nil {
				id := current.ConversationID
				task.publicationConversationID = &id
			}
		}
		plan.conversationID = task.publicationConversationID
	}

	for _, change := range tx.submissionChanges {
		current := tx.submissions[change.id]
		if current == nil {
			record, err := storage.Submission(tx.ctx, change.id)
			if err != nil {
				return nil, err
			}
			current = record
		}
		if current == nil {
			return nil, fmt.Errorf("Submission %d does not exist", change.id)
		}
		next, err := applySubmissionChange(current, change)
		if err != nil {
			return nil, err
		}
		if next != current {
			tx.submissions[change.id] = next
		}
	}

	writes := tx.writes
	for _, value := range tx.submissions {
		writes = append(writes, StorageWrite{Type: "submission", Submission: value})
	}
	for _, task := range tx.tasksByID {
		if task.write != nil {
			writes = append(writes, StorageWrite{Type: "task", Task: task.write})
		}
	}
	for _, plan := range tx.plans {
		change := plan.change
		if plan.content != nil && plan.content.Type == "document.change" && plan.content.DocumentContent.Kind == ContentDelta && change != nil && change.loaded != nil {
			info := CheckpointInfo{DeltasSinceBase: change.loaded.DeltasSinceBase}
			if change.definition != nil && change.definition.CheckpointWhen != nil &&
				change.definition.CheckpointWhen(change.prepared.Value(), change.prepared.Ops(), info) {
				plan.content = &StorageWrite{
					Type: "document.change", DocumentID: plan.content.DocumentID,
					DocumentContent: &DocumentContent{Version: change.version, Kind: ContentBase, Value: jsonValueBytes(change.prepared.Value())},
				}
			}
		}
		if plan.content != nil {
			writes = append(writes, *plan.content)
		}
		if plan.retire {
			writes = append(writes, StorageWrite{Type: "document.retire", DocumentID: planRecordIDPtr(plan)})
		}
	}
	tx.writes = writes
	return writes, nil
}

func (tx *Transaction) validateOwners() error {
	type ownerCheck struct {
		what   string
		taskID Id
	}
	var owners []ownerCheck
	for _, write := range tx.writes {
		if write.Type != "conversation" || write.Conversation == nil || write.Conversation.Owner == nil {
			continue
		}
		owners = append(owners, ownerCheck{what: "Conversation owner task", taskID: write.Conversation.Owner.TaskID})
	}
	for _, task := range tx.tasksByID {
		if task.writeKind == "create" && task.write != nil && task.write.Owner != nil {
			owners = append(owners, ownerCheck{what: "Task owner", taskID: *task.write.Owner})
		}
	}
	for _, owner := range owners {
		task, err := tx.currentTask(owner.taskID)
		if err != nil {
			return err
		}
		if task == nil {
			return fmt.Errorf("%s %d does not exist", owner.what, owner.taskID)
		}
		if task.State.Status == TaskTerminal || task.State.Status == TaskCompleting {
			return fmt.Errorf("%s %d is %s", owner.what, owner.taskID, task.State.Status)
		}
		if task.AbortRequested {
			return fmt.Errorf("%s %d is abort-marked", owner.what, owner.taskID)
		}
	}
	return nil
}

func (tx *Transaction) rejectForkSourceWrites() error {
	for _, plan := range tx.plans {
		if plan.content == nil && !plan.retire {
			continue
		}
		id := planRecordID(plan)
		if _, forbidden := tx.forkSourceDocumentIDs[id]; forbidden {
			return fmt.Errorf("Cannot change fork source document %d in the fork transaction", id)
		}
		scope := planRecordScope(plan)
		if scope != nil && scope.Kind == ScopeConversation && scope.ConversationID != nil {
			if _, forkSource := tx.forkSourceConversationIDs[*scope.ConversationID]; forkSource {
				if record := planRecord(plan); record.Fork != nil && *record.Fork == ForkCurrent {
					return fmt.Errorf("Cannot fork conversation %d while changing its current-policy documents", *scope.ConversationID)
				}
			}
		}
	}
	return nil
}

func (tx *Transaction) abortChanges() {
	for _, document := range tx.documents {
		if document.change != nil {
			document.change.Abort()
		}
	}
}

func (tx *Transaction) assertOpen() error {
	if tx.sealed {
		return fmt.Errorf("Transaction has settled")
	}
	return nil
}

func (tx *Transaction) assertTaskDocumentsOpen(resolved ResolvedAddress) error {
	scope := resolved.Address.Scope
	if scope.Kind == ScopeTask && scope.TaskID != nil {
		if task := tx.tasksByID[*scope.TaskID]; task != nil && task.write != nil && task.write.State.Status == TaskTerminal {
			return fmt.Errorf("Task %d is terminal", *scope.TaskID)
		}
	}
	return nil
}

func (tx *Transaction) read(method string, read func() error) error {
	if err := tx.assertOpen(); err != nil {
		return err
	}
	if tx.hasTableWrite {
		return &ReadAfterWriteError{Method: method}
	}
	return read()
}

func (tx *Transaction) write(operation func() error) error {
	if err := tx.assertOpen(); err != nil {
		return err
	}
	tx.hasTableWrite = true
	return operation()
}

func (tx *Transaction) requireConversation(id Id) error {
	if _, created := tx.createdConversationIDs[id]; created {
		return nil
	}
	record, err := tx.host.Storage().Conversation(tx.ctx, id)
	if err != nil {
		return err
	}
	if record == nil {
		return fmt.Errorf("Conversation %d does not exist", id)
	}
	return nil
}

func (tx *Transaction) taskEntry(id Id) *transactionTask {
	task := tx.tasksByID[id]
	if task == nil {
		task = &transactionTask{}
		tx.tasksByID[id] = task
	}
	return task
}

func (tx *Transaction) currentTask(id Id) (*TaskRecord, error) {
	if task := tx.tasksByID[id]; task != nil && task.write != nil {
		return task.write, nil
	}
	return tx.committedTask(id)
}

func (tx *Transaction) committedTask(id Id) (*TaskRecord, error) {
	task := tx.taskEntry(id)
	if !task.committedLoaded {
		record, err := tx.host.Storage().Task(tx.ctx, id)
		if err != nil {
			return nil, err
		}
		task.committedRead = record
		task.committedLoaded = true
	}
	return task.committedRead, nil
}

// ─── Planning helpers ───────────────────────────────────────────────────────

// planDocument builds the plan of one staged document (upstream planDocument).
func planDocument(document *documentEntry) *documentPlan {
	target := document.target
	if target == nil {
		return nil
	}
	plan := &documentPlan{addressID: document.addressID, retire: document.retireOnCommit}
	switch target.kind {
	case "created":
		record := target.record
		value := jsonValueBytes(document.prepared.Value())
		plan.create = record
		plan.content = &StorageWrite{
			Type: "document.create", DocumentCreate: record,
			DocumentContent: &DocumentContent{Version: target.version, Kind: ContentBase, Value: value},
		}
		plan.change = &planChange{tracker: target.tracker, prepared: document.prepared, version: target.version}
		return plan
	case "fork-copy":
		plan.create = target.record
		plan.content = &StorageWrite{Type: "document.copy", DocumentCreate: target.record, DocumentSource: target.source}
		return plan
	case "retire-only":
		plan.existing = target.retireRecord
		return plan
	case "loaded":
		loaded := target.document
		definition := *document.definition
		prepared := document.prepared
		version := definition.Version
		plan.existing = &loaded.Record
		var content *StorageWrite
		if loaded.StoredVersion < version {
			content = &StorageWrite{
				Type: "document.change", DocumentID: &loaded.Record.ID,
				DocumentContent: &DocumentContent{Version: version, Kind: ContentBase, Value: jsonValueBytes(prepared.Value())},
			}
		} else if len(prepared.Ops()) > 0 {
			delta := DeltaDocumentContent(version, prepared.Ops())
			content = &StorageWrite{
				Type: "document.change", DocumentID: &loaded.Record.ID,
				DocumentContent: &delta,
			}
		}
		plan.content = content
		plan.change = &planChange{
			tracker: loaded.Tracker, prepared: prepared, version: version, loaded: loaded, definition: &definition,
		}
		return plan
	}
	return nil
}

// publishes reports whether adoption publishes the plan (upstream publishes).
func publishes(plan *documentPlan) bool {
	return plan.retire || plan.change == nil || plan.change.loaded == nil || plan.content != nil
}

// applySubmissionChange applies one settlement or placement (upstream
// applySubmissionChange).
func applySubmissionChange(current *SubmissionRecord, change submissionChange) (*SubmissionRecord, error) {
	if current.Status == SubmissionDone || current.Status == SubmissionUnanswered {
		return current, nil
	}
	if change.placement != nil {
		if current.Status != SubmissionQueued {
			return nil, fmt.Errorf("Submission %d is not queued", current.ID)
		}
		next := *current
		entry := *change.placement
		next.Entry = &entry
		if current.Type == SubmissionTypeInput {
			next.Status = SubmissionPlaced
		} else {
			next.Status = SubmissionDone
		}
		return &next, nil
	}
	settlement := change.settlement
	if settlement.Status == SubmissionDone && current.Status != SubmissionPlaced {
		return nil, fmt.Errorf("Submission %d is not a placed input", current.ID)
	}
	next := *current
	next.Status = settlement.Status
	if settlement.Answer != nil {
		answer := *settlement.Answer
		next.Answer = &answer
	}
	if settlement.Reason != nil {
		reason := *settlement.Reason
		next.Reason = &reason
	}
	if settlement.Detail != nil {
		next.Detail = settlement.Detail
	}
	return &next, nil
}

func planRecord(plan *documentPlan) *DocumentRecord {
	if plan.existing != nil {
		return plan.existing
	}
	record := DocumentRecord{
		ID: plan.create.ID, Kind: plan.create.Kind, Key: plan.create.Key, Scope: plan.create.Scope,
		History: plan.create.History, Fork: plan.create.Fork,
	}
	return &record
}

func planRecordID(plan *documentPlan) Id {
	if plan.existing != nil {
		return plan.existing.ID
	}
	return plan.create.ID
}

func planRecordIDPtr(plan *documentPlan) *Id {
	id := planRecordID(plan)
	return &id
}

func planRecordScope(plan *documentPlan) *DocumentScope {
	if plan.existing != nil {
		return &plan.existing.Scope
	}
	if plan.create != nil {
		return &plan.create.Scope
	}
	return nil
}

// jsonValueBytes re-encodes a JSON value for the storage record.
func jsonValueBytes(value chord.JsonValue) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}
