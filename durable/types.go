// Package durable is a Go port of @earendil-works/pi-durable
// (pi/packages/durable): the durable conversation, task, and document runtime.
//
// Ground truth: pi/packages/durable/src at the pinned upstream commit.
//
// D-row D14: upstream's Context (from chord) is the ambient capability handle
// threaded through storage calls; the Go port uses context.Context, and JSON
// payload types (JsonValue, Message[]) map to json.RawMessage and ai.Message.
package durable

import (
	"context"
	"encoding/json"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord/delta"
	"strconv"
)

// Id is a session-global identifier shared by every durable record table.
type Id = int64

// Seq is the monotonic sequence assigned to one atomic storage commit.
type Seq = int64

// RootConversationID is the reserved ID of the root conversation.
const RootConversationID Id = 1

// StoredError is a JSON-safe error snapshot persisted instead of a runtime
// error object.
type StoredError struct {
	Message string          `json:"message"`
	Detail  json.RawMessage `json:"detail,omitempty"`
}

// ConversationParent is the fork source and inclusive parent entry through
// which history is inherited.
type ConversationParent struct {
	ConversationID Id `json:"conversationId"`
	At             Id `json:"at"`
}

// ConversationOwner is the creator edge used for authorization, subtree abort,
// and subtree idle waits.
type ConversationOwner struct {
	ConversationID Id `json:"conversationId"`
	TaskID         Id `json:"taskId"`
}

// ConversationRecord is the immutable identity, history ancestry, and task
// ownership of a transcript scope.
type ConversationRecord struct {
	ID     Id                  `json:"id"`
	Parent *ConversationParent `json:"parent,omitempty"`
	Owner  *ConversationOwner  `json:"owner,omitempty"`
}

// Context edit actions.
const (
	EditOmit    = "omit"
	EditReplace = "replace"
)

// ContextEdit is an immutable override of one visible entry's contribution to
// model context.
type ContextEdit struct {
	// Target is the entry whose model messages are omitted or replaced.
	Target Id     `json:"target"`
	Action string `json:"action"`
	// Messages are contributed instead of the target's messages (replace).
	Messages []ai.Message `json:"messages,omitempty"`
}

// EntryRecord is an immutable transcript event with separate model-facing and
// application-facing payloads.
type EntryRecord struct {
	ID             Id `json:"id"`
	ConversationID Id `json:"conversationId"`
	// Kind is the application-defined entry discriminator.
	Kind string `json:"kind"`
	// Model holds messages contributed to model context (absent for display
	// or bookkeeping entries).
	Model []ai.Message `json:"model,omitempty"`
	// Data is the JSON payload consumed by views, plugins, or bookkeeping.
	Data json.RawMessage `json:"data,omitempty"`
	// Head is the first entry in the active context selected by this entry.
	Head *Id `json:"head,omitempty"`
	// Edits are context-only overrides of earlier visible entries.
	Edits []ContextEdit `json:"edits,omitempty"`
	// ByTaskID is the task that appended this entry, for durable work.
	ByTaskID *Id `json:"byTaskId,omitempty"`
}

// EntryDraft is entry content supplied before the session assigns identity and
// task attribution.
type EntryDraft struct {
	Kind  string          `json:"kind"`
	Model []ai.Message    `json:"model,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
	// Head: nil starts no active context; HeadSelf starts it at the newly
	// assigned entry id; otherwise the given entry id.
	Head     *Id           `json:"head,omitempty"`
	HeadSelf bool          `json:"headSelf,omitempty"`
	Edits    []ContextEdit `json:"edits,omitempty"`
}

// Submission record types and lifecycle statuses (upstream SubmissionRecord).
const (
	SubmissionTypeInput = "input"
	SubmissionTypeWrite = "write"
)

// Submission lifecycle statuses.
const (
	SubmissionQueued     = "queued"
	SubmissionPlaced     = "placed"
	SubmissionDone       = "done"
	SubmissionUnanswered = "unanswered"
)

// SubmissionRecord is the durable lifecycle of one admitted user input or
// passive entry write (upstream SubmissionRecord).
type SubmissionRecord struct {
	ID             Id `json:"id"`
	ConversationID Id `json:"conversationId"`
	// Type is "input" or "write".
	Type string `json:"type"`
	// RequestID is the host-provided deduplication key, scoped to the
	// conversation.
	RequestID *string `json:"requestId,omitempty"`
	// Status is one of queued/placed/done/unanswered.
	Status string `json:"status"`
	// Entry is the user transcript entry created when this submission was
	// placed (placed/done/unanswered).
	Entry *Id `json:"entry,omitempty"`
	// Answer is the assistant answer entry (done, absent for passive writes).
	Answer *Id `json:"answer,omitempty"`
	// Reason is a stable machine-readable explanation ("aborted", "stale").
	Reason *string `json:"reason,omitempty"`
	// Detail is optional structured diagnostic data.
	Detail json.RawMessage `json:"detail,omitempty"`
}

// SubmissionCreate is a submission record before the Session assigns an id
// (upstream SubmissionCreate).
type SubmissionCreate struct {
	ConversationID Id              `json:"conversationId"`
	Type           string          `json:"type"`
	RequestID      *string         `json:"requestId,omitempty"`
	Status         string          `json:"status"`
	Entry          *Id             `json:"entry,omitempty"`
	Answer         *Id             `json:"answer,omitempty"`
	Reason         *string         `json:"reason,omitempty"`
	Detail         json.RawMessage `json:"detail,omitempty"`
}

// SubmissionSettlement is the terminal status staged for a submission
// (upstream SubmissionSettlement): done with an answer, or unanswered with a
// reason.
type SubmissionSettlement struct {
	Status string          `json:"status"`
	Answer *Id             `json:"answer,omitempty"`
	Reason *string         `json:"reason,omitempty"`
	Detail json.RawMessage `json:"detail,omitempty"`
}

// Task outcome statuses.
const (
	OutcomeCompleted = "completed"
	OutcomeFailed    = "failed"
	OutcomeAborted   = "aborted"
	OutcomeOrphaned  = "orphaned"
	OutcomeFaulted   = "faulted"
)

// TaskOutcome is the durable reason and optional result of a terminal task.
type TaskOutcome struct {
	Status string          `json:"status"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *StoredError    `json:"error,omitempty"`
	Reason *string         `json:"reason,omitempty"`
}

// Task state statuses.
const (
	TaskPending  = "pending"
	TaskRunning  = "running"
	TaskTerminal = "terminal"
)

// TaskState is the complete durable execution state of a task.
type TaskState struct {
	// Status is one of pending/running/terminal.
	Status string `json:"status"`
	// Checkpoint is the complete durable state from which execution resumes
	// (pending/running only).
	Checkpoint json.RawMessage `json:"checkpoint,omitempty"`
	// Outcome is set for terminal tasks.
	Outcome *TaskOutcome `json:"outcome,omitempty"`
}

// TaskRecord is the complete replacement record for one durable task state
// machine.
type TaskRecord struct {
	ID             Id `json:"id"`
	ConversationID Id `json:"conversationId"`
	// Kind is the registered task definition name.
	Kind string `json:"kind"`
	// Version is the definition version used to migrate input/checkpoints.
	Version int `json:"version"`
	// Input is the original task input, retained while live or terminal.
	Input json.RawMessage `json:"input,omitempty"`
	// After lists tasks that must be terminal before ordinary execution.
	After []Id `json:"after,omitempty"`
	// Background excludes this task from ordinary idle waits and conversation
	// aborts.
	Background bool `json:"background,omitempty"`
	// AbortRequested is the durable abort mark checked before run-mode
	// progress is committed.
	AbortRequested bool      `json:"abortRequested,omitempty"`
	State          TaskState `json:"state"`
	// Memos are small first-writer-wins values retained while the task is live.
	Memos map[string]json.RawMessage `json:"memos,omitempty"`
}

// Document scope kinds (upstream DocumentRecord["scope"]["kind"]).
const (
	ScopeSession      = "session"
	ScopeConversation = "conversation"
	ScopeTask         = "task"
)

// Document history modes (conversation scope).
const (
	HistoryLatest     = "latest"
	HistoryRewindable = "rewindable"
)

// Document fork modes (conversation scope).
const (
	ForkCurrent = "current"
	ForkInitial = "initial"
	ForkAsOf    = "asOf"
)

// DocumentScope is a document's ownership: session, conversation, or task.
type DocumentScope struct {
	// Kind is session, conversation, or task (see Scope* constants).
	Kind string `json:"kind"`
	// ConversationID applies to conversation-scoped documents.
	ConversationID *Id `json:"conversationId,omitempty"`
	// TaskID applies to task-scoped documents.
	TaskID *Id `json:"taskId,omitempty"`
}

// DocumentRecord is the persisted lifecycle record for one create-to-retire
// document incarnation (upstream DocumentRecord).
type DocumentRecord struct {
	// ID is the unique incarnation ID; never reused when the same logical
	// document is recreated.
	ID Id `json:"id"`
	// Kind is the registered document kind.
	Kind string `json:"kind"`
	// Key is the family member key (absent for singleton documents).
	Key *string `json:"key,omitempty"`
	// CreatedAt is the commit that created the incarnation.
	CreatedAt Seq `json:"createdAt"`
	// RetiredAt is the commit that retired the incarnation; absent while
	// current.
	RetiredAt *Seq `json:"retiredAt,omitempty"`
	// Scope carries the ownership.
	Scope DocumentScope `json:"scope"`
	// History is "latest" or "rewindable" (conversation documents).
	History *string `json:"history,omitempty"`
	// Fork is "current" | "initial" | "asOf" (conversation documents).
	Fork *string `json:"fork,omitempty"`
}

// DocumentCreate is the fields supplied when storage stamps a new document
// (upstream DocumentCreate: the record without its stamps).
type DocumentCreate struct {
	ID      Id            `json:"id"`
	Kind    string        `json:"kind"`
	Key     *string       `json:"key,omitempty"`
	Scope   DocumentScope `json:"scope"`
	History *string       `json:"history,omitempty"`
	Fork    *string       `json:"fork,omitempty"`
}

// Document content kinds.
const (
	ContentBase  = "base"
	ContentDelta = "delta"
)

// DocumentContent is a complete checkpoint (base) or a chord operation batch
// (delta). Ops hold the chord wire tuples (each delta.Op's Tuple()).
type DocumentContent struct {
	// Version is the stored definition version (positive).
	Version int `json:"version"`
	// Kind is ContentBase or ContentDelta.
	Kind string `json:"kind"`
	// Value is the base value; set for base content.
	Value json.RawMessage `json:"value,omitempty"`
	// Ops are the delta wire tuples; set for delta content.
	Ops []any `json:"ops,omitempty"`
}

// BaseDocumentContent builds base content.
func BaseDocumentContent(version int, value json.RawMessage) DocumentContent {
	return DocumentContent{Version: version, Kind: ContentBase, Value: value}
}

// DeltaDocumentContent builds delta content from decoded chord operations.
func DeltaDocumentContent(version int, ops []delta.Op) DocumentContent {
	encoded := make([]any, 0, len(ops))
	for _, op := range ops {
		encoded = append(encoded, op.Tuple())
	}
	return DocumentContent{Version: version, Kind: ContentDelta, Ops: encoded}
}

// DecodedOps decodes the delta's chord operations, or nil for base content.
func (c DocumentContent) DecodedOps() ([]delta.Op, error) {
	if c.Kind != ContentDelta {
		return nil, nil
	}
	wire := make([]delta.WireOp, 0, len(c.Ops))
	for _, tuple := range c.Ops {
		op, err := delta.ParseWireOp(tuple)
		if err != nil {
			return nil, err
		}
		wire = append(wire, op)
	}
	return delta.NewDecoder().Decode(wire)
}

// DocumentPoint selects current state or one historical commit sequence
// (upstream DocumentPoint: Seq | "current").
type DocumentPoint struct {
	// Current selects the current incarnation.
	Current bool
	// Seq is the historical commit sequence when Current is false.
	Seq Seq
}

// CurrentDocumentPoint selects current state.
func CurrentDocumentPoint() DocumentPoint { return DocumentPoint{Current: true} }

// DocumentPointAt selects one commit sequence.
func DocumentPointAt(seq Seq) DocumentPoint { return DocumentPoint{Seq: seq} }

// String renders the point (upstream's "current" | number).
func (p DocumentPoint) String() string {
	if p.Current {
		return "current"
	}
	return strconv.FormatInt(int64(p.Seq), 10)
}

// DocumentAddress is the exact logical identity of a singleton or one keyed
// family member.
type DocumentAddress struct {
	Kind  string
	Scope DocumentScope
	// Key absent selects the singleton; present selects one family member.
	Key *string
}

// DocumentQuery is an ordered scan of document incarnations alive in one exact
// scope at one point.
type DocumentQuery struct {
	Scope DocumentScope
	At    DocumentPoint
	// Kind filters the scan when set.
	Kind *string
}

// DocumentCopySource is the exact persisted source of a definition-free copy.
type DocumentCopySource struct {
	ID Id
	At DocumentPoint
}

// StoredDocument is a detached materialized value with its stored definition
// version.
type StoredDocument struct {
	Record DocumentRecord
	// Version is the stored definition version of the base that was read.
	Version int
	// Value is the materialized value.
	Value json.RawMessage
	// DeltasSinceBase counts the deltas replayed after the selected base.
	DeltasSinceBase int
}

// Cursor is backend-owned JSON continuation state that callers only round-trip
// to the same scan.
type Cursor = map[string]json.RawMessage

// CursorAfter builds a cursor positioned after an id.
func CursorAfter(id Id) Cursor {
	encoded, _ := json.Marshal(id)
	return Cursor{"after": encoded}
}

// Page is one ordered scan result and its optional continuation state.
type Page[T any] struct {
	Items []T
	Next  Cursor
}

// EntryQuery is the inclusive id bounds for a newest-first scan of one
// conversation's fork-aware history.
type EntryQuery struct {
	ConversationID Id
	// MinEntryID is the oldest entry id that may be returned.
	MinEntryID *Id
	// MaxEntryID is the newest entry id that may be returned.
	MaxEntryID *Id
}

// TaskQuery filters an ordered scan of durable task records.
type TaskQuery struct {
	ConversationID *Id
	Kind           *string
	Status         *string
	AbortRequested *bool
	Background     *bool
}

// StorageWrite is one table or document mutation in an atomic storage commit.
// Task and input writes replace whole records; document create/change/retire
// write whole revisions.
type StorageWrite struct {
	// Type is "conversation" | "entry" | "task" | "submission" |
	// "document.create" | "document.copy" | "document.change" |
	// "document.retire".
	Type string

	Conversation *ConversationRecord
	Entry        *EntryRecord
	Task         *TaskRecord
	Submission   *SubmissionRecord

	// DocumentID is the target of a document.change/retire write.
	DocumentID *Id
	// DocumentCreate is the record of a document.create/copy write.
	DocumentCreate *DocumentCreate
	// DocumentContent is the content of a document.create (base) or
	// document.change write.
	DocumentContent *DocumentContent
	// DocumentSource is the copy source of a document.copy write.
	DocumentSource *DocumentCopySource
}

// EntryCommit is one stored entry plus the commit that persisted it.
type EntryCommit struct {
	Entry     EntryRecord
	CommitSeq Seq
}

// Storage is the atomic persistence boundary for session records.
//
// Storage trusts the owning session to supply semantically valid records,
// references, ancestry, and transitions. Implementations enforce atomicity,
// global id ownership, immutable conversation/entry creation, and detached
// values; the session serializes commits.
type Storage interface {
	// Commit atomically persists one batch and returns the sequence assigned
	// to that commit.
	Commit(ctx context.Context, writes []StorageWrite) (Seq, error)

	// MintID returns a fresh candidate from the session-global record id
	// namespace.
	MintID(ctx context.Context) (Id, error)

	// Conversation looks up one conversation by exact id.
	Conversation(ctx context.Context, id Id) (*ConversationRecord, error)

	// ScanConversations scans conversations in ascending id order.
	ScanConversations(ctx context.Context, cursor Cursor, limit int) (Page[ConversationRecord], error)

	// Entry looks up one global entry and the commit that persisted it.
	Entry(ctx context.Context, id Id) (*EntryCommit, error)

	// FindLatestHeadMarker returns the newest visible entry with a head at or
	// below the optional inclusive cutoff.
	FindLatestHeadMarker(ctx context.Context, conversationID Id, atOrBeforeEntryID *Id) (*EntryRecord, error)

	// ScanEntries scans the inclusive visible range newest-first.
	ScanEntries(ctx context.Context, query EntryQuery, cursor Cursor, limit int) (Page[EntryRecord], error)

	// Task looks up the latest complete record for one task.
	Task(ctx context.Context, id Id) (*TaskRecord, error)

	// ScanTasks scans task records matching every supplied filter.
	ScanTasks(ctx context.Context, query TaskQuery, cursor Cursor, limit int) (Page[TaskRecord], error)

	// Submission looks up the latest complete record for one admitted
	// submission.
	Submission(ctx context.Context, id Id) (*SubmissionRecord, error)

	// SubmissionByRequest finds a submission by its conversation-scoped host
	// deduplication key.
	SubmissionByRequest(ctx context.Context, conversationID Id, requestID string) (*SubmissionRecord, error)

	// FindDocument returns the incarnation alive at the point for an exact
	// address (upstream findDocument).
	FindDocument(ctx context.Context, address DocumentAddress, at DocumentPoint) (*DocumentRecord, error)

	// Document materializes one incarnation at the point (upstream document).
	Document(ctx context.Context, id Id, at DocumentPoint) (*StoredDocument, error)

	// ScanDocuments scans the incarnations alive in one scope at the point, in
	// ascending id order (upstream scanDocuments).
	ScanDocuments(ctx context.Context, query DocumentQuery, cursor Cursor, limit int) (Page[DocumentRecord], error)

	// Close releases backend resources; all later operations must reject.
	Close(ctx context.Context) error
}
