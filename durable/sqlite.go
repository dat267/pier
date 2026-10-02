package durable

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	ai "github.com/dat267/pier/ai"

	// Pure-Go SQLite driver (user-approved dependency; no cgo).
	_ "modernc.org/sqlite"
)

// SqliteStorage is the SQLite-backed Storage backend (upstream
// storage/sqlite). One connection serializes every operation, mirroring the
// upstream adapters' queued execution.
type SqliteStorage struct {
	db *sql.DB
}

// OpenSqliteStorage opens (creating if needed) the database at path and
// applies pending migrations.
func OpenSqliteStorage(path string) (*SqliteStorage, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// Serialize: transactions queue unrelated work (upstream's SqliteDatabase
	// contract).
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON"); err != nil {
		_ = db.Close()
		return nil, err
	}
	storage := &SqliteStorage{db: db}
	if err := storage.applyMigrations(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return storage, nil
}

// NewSqliteStorage wraps an open database handle.
func NewSqliteStorage(db *sql.DB) (*SqliteStorage, error) {
	db.SetMaxOpenConns(1)
	storage := &SqliteStorage{db: db}
	if err := storage.applyMigrations(); err != nil {
		return nil, err
	}
	return storage, nil
}

// sqliteMigrations mirrors upstream migrations.ts: one migration, contiguous
// version 1. `submissions.request_id`/`status` carry the submission lifecycle
// (the record's own JSON carries its type); upstream's task-status CHECK
// matches the port's states.
const sqliteInitialSchema = `
CREATE TABLE durable_metadata (
	singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
	next_id TEXT NOT NULL,
	next_seq INTEGER NOT NULL
) STRICT;
INSERT INTO durable_metadata (singleton, next_id, next_seq) VALUES (1, '2', 1);
CREATE TABLE record_ids (
	id INTEGER PRIMARY KEY,
	record_type TEXT NOT NULL CHECK (record_type IN ('conversation', 'entry', 'task', 'submission', 'document'))
) STRICT;
CREATE TABLE conversations (
	id INTEGER PRIMARY KEY,
	owner_conversation_id INTEGER,
	owner_task_id INTEGER,
	record TEXT NOT NULL CHECK (json_valid(record))
) STRICT;
CREATE INDEX conversations_by_owner_conversation ON conversations (owner_conversation_id, id);
CREATE INDEX conversations_by_owner_task ON conversations (owner_task_id, id);
CREATE TABLE entries (
	id INTEGER PRIMARY KEY,
	conversation_id INTEGER NOT NULL,
	head INTEGER,
	commit_seq INTEGER NOT NULL,
	record TEXT NOT NULL CHECK (json_valid(record))
) STRICT;
CREATE INDEX entries_by_conversation ON entries (conversation_id, id DESC);
CREATE INDEX entry_heads_by_conversation ON entries (conversation_id, id DESC) WHERE head IS NOT NULL;
CREATE TABLE tasks (
	id INTEGER PRIMARY KEY,
	conversation_id INTEGER NOT NULL,
	kind TEXT NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'waiting', 'completing', 'terminal')),
	abort_requested INTEGER NOT NULL CHECK (abort_requested IN (0, 1)),
	background INTEGER NOT NULL CHECK (background IN (0, 1)),
	record TEXT NOT NULL CHECK (json_valid(record))
) STRICT;
CREATE INDEX tasks_by_status ON tasks (status, id);
CREATE INDEX tasks_by_conversation ON tasks (conversation_id, id);
CREATE INDEX tasks_by_kind ON tasks (kind, id);
CREATE INDEX tasks_by_abort_requested ON tasks (abort_requested, id);
CREATE INDEX tasks_by_background ON tasks (background, id);
CREATE TABLE submissions (
	id INTEGER PRIMARY KEY,
	conversation_id INTEGER NOT NULL,
	request_id TEXT,
	status TEXT NOT NULL CHECK (status IN ('queued', 'placed', 'done', 'unanswered')),
	record TEXT NOT NULL CHECK (json_valid(record))
) STRICT;
CREATE INDEX submissions_by_request ON submissions (conversation_id, request_id);
CREATE INDEX submissions_by_conversation ON submissions (conversation_id, id);
CREATE INDEX submissions_by_status ON submissions (status, id);
CREATE TABLE documents (
	id INTEGER PRIMARY KEY,
	kind TEXT NOT NULL,
	family INTEGER NOT NULL CHECK (family IN (0, 1)),
	key_value TEXT NOT NULL,
	scope_kind TEXT NOT NULL CHECK (scope_kind IN ('session', 'conversation', 'task')),
	owner_id INTEGER NOT NULL,
	created_at INTEGER NOT NULL,
	retired_at INTEGER,
	record TEXT NOT NULL CHECK (json_valid(record))
) STRICT;
CREATE INDEX documents_by_address
	ON documents (kind, scope_kind, owner_id, family, key_value, created_at DESC, retired_at);
CREATE INDEX documents_by_scope ON documents (scope_kind, owner_id, id);
CREATE INDEX documents_by_scope_kind ON documents (scope_kind, owner_id, kind, id);
CREATE TABLE document_revisions (
	document_id INTEGER NOT NULL,
	seq INTEGER NOT NULL,
	kind TEXT NOT NULL CHECK (kind IN ('base', 'delta')),
	version INTEGER NOT NULL,
	content TEXT NOT NULL CHECK (json_valid(content)),
	PRIMARY KEY (document_id, seq)
) STRICT;
CREATE INDEX document_revisions_by_kind ON document_revisions (document_id, kind, seq DESC);
`

// sqliteCurrentSchemaVersion mirrors CURRENT_SQLITE_SCHEMA_VERSION.
const sqliteCurrentSchemaVersion = 1

// applyMigrations applies pending schema migrations atomically (upstream
// applySqliteMigrations).
func (s *SqliteStorage) applyMigrations() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS durable_schema (
		singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
		version INTEGER NOT NULL CHECK (version >= 0)
	) STRICT`); err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT OR IGNORE INTO durable_schema (singleton, version) VALUES (1, 0)"); err != nil {
		return err
	}
	var version int
	if err := tx.QueryRow("SELECT version FROM durable_schema WHERE singleton = 1").Scan(&version); err != nil {
		return fmt.Errorf("Durable SQLite schema metadata is missing")
	}
	if version > sqliteCurrentSchemaVersion {
		return fmt.Errorf("Durable SQLite schema version %d is newer than supported version %d", version, sqliteCurrentSchemaVersion)
	}
	if version < 1 {
		if _, err := tx.Exec(sqliteInitialSchema); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE durable_schema SET version = 1 WHERE singleton = 1"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Commit atomically persists one batch (upstream storage.ts commit).
func (s *SqliteStorage) Commit(ctx context.Context, writes []StorageWrite) (Seq, error) {
	if err := s.assertOpen(); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var nextIDText string
	var nextSeq int64
	if err := tx.QueryRowContext(ctx, "SELECT next_id, next_seq FROM durable_metadata WHERE singleton = 1").Scan(&nextIDText, &nextSeq); err != nil {
		return 0, err
	}
	if err := checkImmutableIDs(writes, recordTypeOf, func(id Id) (string, error) {
		var recordType string
		err := tx.QueryRowContext(ctx, "SELECT record_type FROM record_ids WHERE id = ?", id).Scan(&recordType)
		if err == sql.ErrNoRows {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		return recordType, nil
	}); err != nil {
		return 0, err
	}

	for _, write := range writes {
		switch write.Type {
		case "conversation":
			value := write.Conversation
			record, err := marshalJSONValue(value)
			if err != nil {
				return 0, err
			}
			var ownerConversation, ownerTask any
			if value.Owner != nil {
				ownerConversation = value.Owner.ConversationID
				ownerTask = value.Owner.TaskID
			}
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO conversations (id, owner_conversation_id, owner_task_id, record) VALUES (?, ?, ?, ?)",
				value.ID, ownerConversation, ownerTask, record); err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO record_ids (id, record_type) VALUES (?, 'conversation')", value.ID); err != nil {
				return 0, err
			}
		case "entry":
			value := write.Entry
			record, err := marshalJSONValue(value)
			if err != nil {
				return 0, err
			}
			var head any
			if value.Head != nil {
				head = *value.Head
			}
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO entries (id, conversation_id, head, commit_seq, record) VALUES (?, ?, ?, ?, ?)",
				value.ID, value.ConversationID, head, nextSeq, record); err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO record_ids (id, record_type) VALUES (?, 'entry')", value.ID); err != nil {
				return 0, err
			}
		case "task":
			value := write.Task
			record, err := marshalJSONValue(value)
			if err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx,
				"INSERT OR REPLACE INTO tasks (id, conversation_id, kind, status, abort_requested, background, record) VALUES (?, ?, ?, ?, ?, ?, ?)",
				value.ID, value.ConversationID, value.Kind, value.State.Status, boolInt(value.AbortRequested), boolInt(value.Background), record); err != nil {
				return 0, err
			}
			if err := claimRecordID(ctx, tx, value.ID, "task"); err != nil {
				return 0, err
			}
		// Documents are applied once per batch (one revision per document per
		// commit), so they are collected and handled after the table writes.
		case "document.create", "document.copy", "document.change", "document.retire":
			continue
		case "submission":
			value := write.Submission
			record, err := marshalJSONValue(value)
			if err != nil {
				return 0, err
			}
			// The request mapping tracks the latest record only (the memory
			// backend deletes the previous mapping on re-write).
			if _, err := tx.ExecContext(ctx, "DELETE FROM submissions WHERE id = ?", value.ID); err != nil {
				return 0, err
			}
			var requestID any
			if value.RequestID != nil {
				requestID = *value.RequestID
			}
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO submissions (id, conversation_id, request_id, status, record) VALUES (?, ?, ?, ?, ?)",
				value.ID, value.ConversationID, requestID, value.Status, record); err != nil {
				return 0, err
			}
			if err := claimRecordID(ctx, tx, value.ID, "submission"); err != nil {
				return 0, err
			}
		default:
			return 0, fmt.Errorf("unknown storage write type: %s", write.Type)
		}
	}

	if err := commitSqliteDocuments(ctx, tx, writes, Seq(nextSeq)); err != nil {
		return 0, err
	}

	mintedID, err := parseMetadataID(nextIDText)
	if err != nil {
		return 0, err
	}
	for _, write := range writes {
		if id := writeID(write); id+1 > mintedID {
			mintedID = id + 1
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE durable_metadata SET next_id = ?, next_seq = ? WHERE singleton = 1",
		formatMetadataID(mintedID), nextSeq+1); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return Seq(nextSeq), nil
}

// recordTypeOf maps the write type to the storage table's record type (the
// document write kinds share one record type).
func recordTypeOf(write StorageWrite) string {
	switch write.Type {
	case "document.create", "document.copy", "document.change", "document.retire":
		return "document"
	}
	return write.Type
}

// claimRecordID inserts the ownership row unless the id already exists as
// the same record type.
func claimRecordID(ctx context.Context, tx *sql.Tx, id Id, recordType string) error {
	var existing string
	err := tx.QueryRowContext(ctx, "SELECT record_type FROM record_ids WHERE id = ?", id).Scan(&existing)
	if err == sql.ErrNoRows {
		_, err = tx.ExecContext(ctx, "INSERT INTO record_ids (id, record_type) VALUES (?, ?)", id, recordType)
		return err
	}
	if err != nil {
		return err
	}
	if existing != recordType {
		return fmt.Errorf("ID %d already belongs to %s", id, existing)
	}
	return nil
}

// MintID returns the next id from the global namespace (upstream mintId).
func (s *SqliteStorage) MintID(ctx context.Context) (Id, error) {
	if err := s.assertOpen(); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var nextIDText string
	if err := tx.QueryRowContext(ctx, "SELECT next_id FROM durable_metadata WHERE singleton = 1").Scan(&nextIDText); err != nil {
		return 0, err
	}
	mintedID, err := parseMetadataID(nextIDText)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE durable_metadata SET next_id = ? WHERE singleton = 1", formatMetadataID(mintedID+1)); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return mintedID, nil
}

// Conversation looks up one conversation by exact id.
func (s *SqliteStorage) Conversation(ctx context.Context, id Id) (*ConversationRecord, error) {
	if err := s.assertOpen(); err != nil {
		return nil, err
	}
	var record string
	err := s.db.QueryRowContext(ctx, "SELECT record FROM conversations WHERE id = ?", id).Scan(&record)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return unmarshalRecord[ConversationRecord](record)
}

// ScanConversations scans conversations in ascending id order.
func (s *SqliteStorage) ScanConversations(ctx context.Context, filter ConversationQuery, cursor Cursor, limit int) (Page[ConversationRecord], error) {
	if err := s.assertOpen(); err != nil {
		return Page[ConversationRecord]{}, err
	}
	after, err := sqliteCursorID(cursor)
	if err != nil {
		return Page[ConversationRecord]{}, err
	}
	query := "SELECT id, record FROM conversations"
	conditions := []string{}
	args := []any{}
	if after != nil {
		conditions = append(conditions, "id > ?")
		args = append(args, *after)
	}
	if filter.OwnerConversationID != nil {
		conditions = append(conditions, "owner_conversation_id = ?")
		args = append(args, *filter.OwnerConversationID)
	}
	if filter.OwnerTaskID != nil {
		conditions = append(conditions, "owner_task_id = ?")
		args = append(args, *filter.OwnerTaskID)
	}
	if len(conditions) > 0 {
		query += " WHERE " + joinConditions(conditions)
	}
	query += " ORDER BY id LIMIT ?"
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Page[ConversationRecord]{}, err
	}
	defer rows.Close()
	var values []ConversationRecord
	for rows.Next() {
		var id Id
		var record string
		if err := rows.Scan(&id, &record); err != nil {
			return Page[ConversationRecord]{}, err
		}
		decoded, err := unmarshalRecord[ConversationRecord](record)
		if err != nil {
			return Page[ConversationRecord]{}, err
		}
		values = append(values, *decoded)
	}
	if err := rows.Err(); err != nil {
		return Page[ConversationRecord]{}, err
	}
	return pageOf(values, limit, func(value ConversationRecord) Id { return value.ID }), nil
}

// Entry looks up one entry and its commit sequence.
func (s *SqliteStorage) Entry(ctx context.Context, id Id) (*EntryCommit, error) {
	if err := s.assertOpen(); err != nil {
		return nil, err
	}
	var record string
	var commitSeq int64
	err := s.db.QueryRowContext(ctx, "SELECT record, commit_seq FROM entries WHERE id = ?", id).Scan(&record, &commitSeq)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entry, err := unmarshalRecord[EntryRecord](record)
	if err != nil {
		return nil, err
	}
	return &EntryCommit{Entry: *entry, CommitSeq: Seq(commitSeq)}, nil
}

// FindLatestHeadMarker walks the fork chain for the newest head marker at or
// below the cutoff.
func (s *SqliteStorage) FindLatestHeadMarker(ctx context.Context, conversationID Id, atOrBeforeEntryID *Id) (*EntryRecord, error) {
	if err := s.assertOpen(); err != nil {
		return nil, err
	}
	exists, err := s.conversationExists(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("Unknown conversation: %d", conversationID)
	}
	upperEntryID := Id(math.MaxInt64)
	if atOrBeforeEntryID != nil {
		upperEntryID = *atOrBeforeEntryID
	}
	currentID := conversationID
	for {
		var record string
		err := s.db.QueryRowContext(ctx,
			"SELECT record FROM entries WHERE conversation_id = ? AND head IS NOT NULL AND id <= ? ORDER BY id DESC LIMIT 1",
			currentID, upperEntryID).Scan(&record)
		if err == nil {
			return unmarshalRecord[EntryRecord](record)
		}
		if err != sql.ErrNoRows {
			return nil, err
		}
		parent, err := s.parentOf(ctx, currentID)
		if err != nil {
			return nil, err
		}
		if parent == nil {
			return nil, nil
		}
		if parent.At < upperEntryID {
			upperEntryID = parent.At
		}
		if upperEntryID < math.MinInt64 {
			return nil, nil
		}
		currentID = parent.ConversationID
	}
}

// ScanEntries scans the inclusive visible range newest-first (fork-aware,
// like the memory backend's visibleEntries walk).
func (s *SqliteStorage) ScanEntries(ctx context.Context, query EntryQuery, cursor Cursor, limit int) (Page[EntryRecord], error) {
	if err := s.assertOpen(); err != nil {
		return Page[EntryRecord]{}, err
	}
	maxEntryID := Id(math.MaxInt64)
	if query.MaxEntryID != nil {
		maxEntryID = *query.MaxEntryID
	}
	after, err := sqliteCursorID(cursor)
	if err != nil {
		return Page[EntryRecord]{}, err
	}
	if after != nil && *after-1 < maxEntryID {
		maxEntryID = *after - 1
	}
	minEntryID := Id(math.MinInt64)
	if query.MinEntryID != nil {
		minEntryID = *query.MinEntryID
	}
	exists, err := s.conversationExists(ctx, query.ConversationID)
	if err != nil {
		return Page[EntryRecord]{}, err
	}
	if !exists {
		return Page[EntryRecord]{}, nil
	}
	var visible []EntryRecord
	currentID := query.ConversationID
	upperEntryID := maxEntryID
	for len(visible) <= limit {
		rows, err := s.db.QueryContext(ctx,
			"SELECT id, record FROM entries WHERE conversation_id = ? AND id <= ? ORDER BY id DESC",
			currentID, upperEntryID)
		if err != nil {
			return Page[EntryRecord]{}, err
		}
		stop := false
		for rows.Next() {
			var id Id
			var record string
			if err := rows.Scan(&id, &record); err != nil {
				rows.Close()
				return Page[EntryRecord]{}, err
			}
			if id < minEntryID {
				stop = true
				break
			}
			entry, err := unmarshalRecord[EntryRecord](record)
			if err != nil {
				rows.Close()
				return Page[EntryRecord]{}, err
			}
			visible = append(visible, *entry)
			if len(visible) > limit {
				break
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return Page[EntryRecord]{}, err
		}
		rows.Close()
		if stop || len(visible) > limit {
			break
		}
		parent, err := s.parentOf(ctx, currentID)
		if err != nil {
			return Page[EntryRecord]{}, err
		}
		if parent == nil {
			break
		}
		if parent.At < upperEntryID {
			upperEntryID = parent.At
		}
		if upperEntryID < minEntryID {
			break
		}
		currentID = parent.ConversationID
	}
	return pageOf(visible, limit, func(value EntryRecord) Id { return value.ID }), nil
}

func (s *SqliteStorage) conversationExists(ctx context.Context, id Id) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM conversations WHERE id = ?", id).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// parentOf returns the fork parent, walking the conversations table.
func (s *SqliteStorage) parentOf(ctx context.Context, id Id) (*ConversationParent, error) {
	var record string
	err := s.db.QueryRowContext(ctx, "SELECT record FROM conversations WHERE id = ?", id).Scan(&record)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("Unknown conversation: %d", id)
	}
	if err != nil {
		return nil, err
	}
	conversation, err := unmarshalRecord[ConversationRecord](record)
	if err != nil {
		return nil, err
	}
	return conversation.Parent, nil
}

// Task looks up the latest record for one task.
func (s *SqliteStorage) Task(ctx context.Context, id Id) (*TaskRecord, error) {
	if err := s.assertOpen(); err != nil {
		return nil, err
	}
	var record string
	err := s.db.QueryRowContext(ctx, "SELECT record FROM tasks WHERE id = ?", id).Scan(&record)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return unmarshalRecord[TaskRecord](record)
}

// ScanTasks scans task records matching every supplied filter.
func (s *SqliteStorage) ScanTasks(ctx context.Context, query TaskQuery, cursor Cursor, limit int) (Page[TaskRecord], error) {
	if err := s.assertOpen(); err != nil {
		return Page[TaskRecord]{}, err
	}
	conditions := []string{"1 = 1"}
	args := []any{}
	if query.ConversationID != nil {
		conditions = append(conditions, "conversation_id = ?")
		args = append(args, *query.ConversationID)
	}
	if query.Kind != nil {
		conditions = append(conditions, "kind = ?")
		args = append(args, *query.Kind)
	}
	if query.Status != nil {
		conditions = append(conditions, "status = ?")
		args = append(args, *query.Status)
	}
	if query.AbortRequested != nil {
		conditions = append(conditions, "abort_requested = ?")
		args = append(args, boolInt(*query.AbortRequested))
	}
	if query.Background != nil {
		conditions = append(conditions, "background = ?")
		args = append(args, boolInt(*query.Background))
	}
	after, err := sqliteCursorID(cursor)
	if err != nil {
		return Page[TaskRecord]{}, err
	}
	if after != nil {
		conditions = append(conditions, "id > ?")
		args = append(args, *after)
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx,
		"SELECT record FROM tasks WHERE "+strings.Join(conditions, " AND ")+" ORDER BY id LIMIT ?",
		args...)
	if err != nil {
		return Page[TaskRecord]{}, err
	}
	defer rows.Close()
	var values []TaskRecord
	for rows.Next() {
		var record string
		if err := rows.Scan(&record); err != nil {
			return Page[TaskRecord]{}, err
		}
		decoded, err := unmarshalRecord[TaskRecord](record)
		if err != nil {
			return Page[TaskRecord]{}, err
		}
		values = append(values, *decoded)
	}
	if err := rows.Err(); err != nil {
		return Page[TaskRecord]{}, err
	}
	return pageOf(values, limit, func(value TaskRecord) Id { return value.ID }), nil
}

// Submission looks up the latest record for one submission.
func (s *SqliteStorage) Submission(ctx context.Context, id Id) (*SubmissionRecord, error) {
	if err := s.assertOpen(); err != nil {
		return nil, err
	}
	var record string
	err := s.db.QueryRowContext(ctx, "SELECT record FROM submissions WHERE id = ?", id).Scan(&record)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return unmarshalRecord[SubmissionRecord](record)
}

// SubmissionByRequest finds a submission by its conversation-scoped
// deduplication key.
func (s *SqliteStorage) SubmissionByRequest(ctx context.Context, conversationID Id, requestID string) (*SubmissionRecord, error) {
	if err := s.assertOpen(); err != nil {
		return nil, err
	}
	var record string
	err := s.db.QueryRowContext(ctx,
		"SELECT record FROM submissions WHERE conversation_id = ? AND request_id = ? ORDER BY id DESC LIMIT 1",
		conversationID, requestID).Scan(&record)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return unmarshalRecord[SubmissionRecord](record)
}

// Close releases the backend; later operations reject.
func (s *SqliteStorage) Close(ctx context.Context) error {
	_ = ctx
	return s.db.Close()
}

func (s *SqliteStorage) assertOpen() error {
	if err := s.db.Ping(); err != nil {
		return fmt.Errorf("SqliteStorage is closed")
	}
	return nil
}

// sqliteCursorID decodes the cursor's `after` id.
func sqliteCursorID(cursor Cursor) (*Id, error) {
	if cursor == nil {
		return nil, nil
	}
	raw, ok := cursor["after"]
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	var after float64
	if err := unmarshalBytes(raw, &after); err != nil {
		return nil, fmt.Errorf("Invalid storage cursor")
	}
	id := Id(after)
	if float64(id) != after {
		return nil, fmt.Errorf("Invalid storage cursor")
	}
	return &id, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// marshalJSONValue encodes a record; entries carry ai.Message payloads, so
// the ai encoder (which preserves wire key order) is used.
func marshalJSONValue(value any) (string, error) {
	if _, ok := value.(EntryRecord); ok {
		enc, err := ai.MarshalJSON(value)
		return string(enc), err
	}
	enc, err := marshalJSON(value)
	return string(enc), err
}

func unmarshalRecord[T any](record string) (*T, error) {
	var decoded T
	if err := unmarshalJSONString(record, &decoded); err != nil {
		return nil, err
	}
	return &decoded, nil
}

// checkImmutableIDs enforces global id ownership and immutable
// conversation/entry creation (shared with the memory backend's semantics;
// the lookup is parameterized for SQL).
func checkImmutableIDs(writes []StorageWrite, recordType func(StorageWrite) string, tableContaining func(Id) (string, error)) error {
	claimed := map[Id]string{}
	for _, write := range writes {
		if write.Type == "document.change" || write.Type == "document.retire" {
			continue
		}
		table := recordType(write)
		id := writeID(write)
		existing, err := tableContaining(id)
		if err != nil {
			return err
		}
		earlier, claimedBefore := claimed[id]
		if table == "conversation" || table == "entry" {
			if existing != "" {
				return fmt.Errorf("ID %d already belongs to %s", id, existing)
			}
			if claimedBefore {
				return fmt.Errorf("ID %d is written more than once", id)
			}
		} else {
			if existing != "" && existing != table {
				return fmt.Errorf("ID %d already belongs to %s", id, existing)
			}
			if claimedBefore && earlier != table {
				return fmt.Errorf("ID %d is written as two record types", id)
			}
		}
		claimed[id] = table
	}
	return nil
}

// marshalJSON encodes a value (small helper over encoding/json).
func marshalJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}

// unmarshalJSONString decodes a record string.
func unmarshalJSONString(record string, target any) error {
	return json.Unmarshal([]byte(record), target)
}

// unmarshalBytes decodes raw JSON bytes.
func unmarshalBytes(data []byte, target any) error {
	return json.Unmarshal(data, target)
}

// parseMetadataID decodes the TEXT next_id column.
func parseMetadataID(text string) (Id, error) {
	var value float64
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return 0, fmt.Errorf("Invalid durable metadata next_id: %s", text)
	}
	id := Id(value)
	if float64(id) != value {
		return 0, fmt.Errorf("Invalid durable metadata next_id: %s", text)
	}
	return id, nil
}

// formatMetadataID encodes the TEXT next_id column.
func formatMetadataID(id Id) string {
	return strconv.FormatInt(int64(id), 10)
}
