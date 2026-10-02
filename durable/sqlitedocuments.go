package durable

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/dat267/pier/chord/delta"
)

// Document operations of the SQLite backend (upstream
// storage/sqlite/storage.ts): the documents and document_revisions tables
// already carry the upstream schema; this file adds the commit arms and the
// read/materialization paths.

// sqliteDocumentRow is the persisted document record plus its stamps.
type sqliteDocumentRow struct {
	record    DocumentRecord
	retiredAt *Seq
}

// commitSqliteDocuments applies the batch's merged document actions inside the
// commit transaction (upstream's storage applies one revision per document per
// commit; the memories of the same batch are merged first).
func commitSqliteDocuments(ctx context.Context, tx *sql.Tx, writes []StorageWrite, seq Seq) error {
	actions, err := mergeDocumentActions(writes)
	if err != nil {
		return err
	}
	// Retirements in this batch release their address for a create in the same
	// batch (the lifecycle case: recreate an address and retire the old
	// incarnation atomically). A copy may not read a document the same batch
	// changes (upstream resolveDocumentCopies).
	batchRetired := map[Id]bool{}
	batchChanged := map[Id]bool{}
	for id, action := range actions {
		if action.retire && action.create == nil {
			batchRetired[id] = true
		}
		if action.create != nil || action.content != nil {
			batchChanged[id] = true
		}
	}
	for _, action := range actions {
		if action.copySource != nil && batchChanged[action.copySource.ID] {
			return fmt.Errorf("Fork source document %d is changed in the copy batch", action.copySource.ID)
		}
	}
	ids := make([]Id, 0, len(actions))
	for id := range actions {
		ids = append(ids, id)
	}
	sortIds(ids)
	for _, id := range ids {
		action := actions[id]
		row, err := sqliteDocumentTx(ctx, tx, id)
		if err != nil {
			return err
		}
		switch {
		case action.create != nil:
			record := action.create
			if row != nil {
				return fmt.Errorf("Document %d already exists", id)
			}
			content := action.content
			if action.copySource != nil {
				stored, err := sqliteMaterializeDocumentTx(ctx, tx, action.copySource.ID, action.copySource.At)
				if err != nil {
					return fmt.Errorf("Document copy %d was rejected: %w", record.ID, err)
				}
				if stored == nil {
					return fmt.Errorf("Fork source document %d cannot be read", action.copySource.ID)
				}
				if stored.Record.Scope.Kind != ScopeConversation || record.Scope.Kind != ScopeConversation ||
					stored.Record.Kind != record.Kind || !sameOptionalString(stored.Record.Key, record.Key) ||
					!sameOptionalString(stored.Record.Scope.History, record.Scope.History) ||
					!sameOptionalString(stored.Record.Scope.Fork, record.Scope.Fork) {
					return fmt.Errorf("Fork source document %d does not match the copied record", action.copySource.ID)
				}
				copied := DocumentContent{Version: stored.Version, Kind: ContentBase, Value: stored.Value}
				content = &copied
			}
			if content == nil || content.Kind != ContentBase {
				return fmt.Errorf("Document %d creation requires base content", record.ID)
			}
			// One current incarnation per exact address; a current incarnation
			// retired in this batch does not count.
			address := DocumentAddress{Kind: record.Kind, Scope: record.Scope, Key: record.Key}
			existing, err := sqliteFindDocumentTx(ctx, tx, address, CurrentDocumentPoint())
			if err != nil {
				return err
			}
			if existing != nil && !batchRetired[existing.ID] {
				return fmt.Errorf("Document address already has a current incarnation")
			}
			var retiredAt any
			if action.retire {
				retiredAt = seq
			}
			recordJSON := marshalRecordString(DocumentRecord{
				ID: record.ID, Kind: record.Kind, Key: record.Key, Scope: record.Scope,
			})
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO documents (id, kind, family, key_value, scope_kind, owner_id, created_at, retired_at, record)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				record.ID, record.Kind, sqliteBool(record.Key != nil), sqliteKeyValue(record.Key),
				record.Scope.Kind, sqliteDocumentOwner(record.Scope), seq, retiredAt, recordJSON); err != nil {
				return err
			}
			if err := claimRecordID(ctx, tx, record.ID, "document"); err != nil {
				return err
			}
			if err := insertSqliteRevision(ctx, tx, record.ID, seq, *content); err != nil {
				return err
			}
			if action.retire && isCurrentOnlyDocument(&DocumentRecord{Scope: record.Scope}) {
				if _, err := tx.ExecContext(ctx, "DELETE FROM document_revisions WHERE document_id = ?", record.ID); err != nil {
					return err
				}
			}
		case action.content != nil:
			if row == nil {
				return fmt.Errorf("Unknown document: %d", id)
			}
			if row.retiredAt != nil && !batchRetired[id] {
				return fmt.Errorf("Document %d is retired", id)
			}
			if action.content.Kind == ContentDelta && !batchRetired[id] {
				last, err := sqliteLastRevisionTx(ctx, tx, id)
				if err != nil {
					return err
				}
				if last == nil {
					return fmt.Errorf("Document %d delta has no base", id)
				}
				if last.Version != action.content.Version {
					return fmt.Errorf("Document %d version transition requires a base", id)
				}
			}
			if err := insertSqliteRevision(ctx, tx, id, seq, *action.content); err != nil {
				return err
			}
			if action.retire {
				if _, err := tx.ExecContext(ctx, "UPDATE documents SET retired_at = ? WHERE id = ? AND retired_at IS NULL", seq, id); err != nil {
					return err
				}
				if isCurrentOnlyDocument(&row.record) {
					if _, err := tx.ExecContext(ctx, "DELETE FROM document_revisions WHERE document_id = ?", id); err != nil {
						return err
					}
				}
			}
		case action.retire:
			if row == nil {
				return fmt.Errorf("Unknown document: %d", id)
			}
			if row.retiredAt != nil {
				return fmt.Errorf("Document %d is retired", id)
			}
			if _, err := tx.ExecContext(ctx, "UPDATE documents SET retired_at = ? WHERE id = ? AND retired_at IS NULL", seq, id); err != nil {
				return err
			}
			if isCurrentOnlyDocument(&row.record) {
				if _, err := tx.ExecContext(ctx, "DELETE FROM document_revisions WHERE document_id = ?", id); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("empty document action for %d", id)
		}
	}
	return nil
}

// sortIds orders ids ascending.
func sortIds(ids []Id) {
	for index := 1; index < len(ids); index++ {
		for current := index; current > 0 && ids[current] < ids[current-1]; current-- {
			ids[current], ids[current-1] = ids[current-1], ids[current]
		}
	}
}

// insertSqliteRevision appends one base or delta.
func insertSqliteRevision(ctx context.Context, tx *sql.Tx, id Id, seq Seq, content DocumentContent) error {
	encoded := content
	if content.Kind == ContentBase {
		// Keep the base value as raw JSON in the content column.
		value := content.Value
		if len(value) == 0 {
			value = json.RawMessage("{}")
		}
		encoded = DocumentContent{Version: content.Version, Kind: ContentBase, Value: value}
	}
	contentJSON, err := json.Marshal(encoded)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		"INSERT INTO document_revisions (document_id, seq, kind, version, content) VALUES (?, ?, ?, ?, ?)",
		id, seq, content.Kind, content.Version, string(contentJSON))
	return err
}

// sqliteDocumentTx reads one document row inside a transaction.
func sqliteDocumentTx(ctx context.Context, tx *sql.Tx, id Id) (*sqliteDocumentRow, error) {
	var recordJSON string
	var createdAt int64
	var retiredAt sql.NullInt64
	err := tx.QueryRowContext(ctx, "SELECT record, created_at, retired_at FROM documents WHERE id = ?", id).
		Scan(&recordJSON, &createdAt, &retiredAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	record, err := unmarshalRecord[DocumentRecord](recordJSON)
	if err != nil {
		return nil, err
	}
	// The stamps live in their own columns; the record column holds the
	// creation fields.
	record.CreatedAt = Seq(createdAt)
	row := &sqliteDocumentRow{record: *record}
	if retiredAt.Valid {
		retired := Seq(retiredAt.Int64)
		row.retiredAt = &retired
		row.record.RetiredAt = &retired
	}
	return row, nil
}

// sqliteFindDocumentTx resolves an exact address at a point inside a
// transaction.
func sqliteFindDocumentTx(ctx context.Context, tx *sql.Tx, address DocumentAddress, at DocumentPoint) (*DocumentRecord, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT record, created_at, retired_at FROM documents
		 WHERE kind = ? AND scope_kind = ? AND owner_id = ? AND family = ? AND key_value = ?
		 ORDER BY id`, address.Kind, address.Scope.Kind, sqliteDocumentOwner(address.Scope),
		sqliteBool(address.Key != nil), sqliteKeyValue(address.Key))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var last *DocumentRecord
	for rows.Next() {
		var recordJSON string
		var createdAt int64
		var retiredAt sql.NullInt64
		if err := rows.Scan(&recordJSON, &createdAt, &retiredAt); err != nil {
			return nil, err
		}
		record, err := unmarshalRecord[DocumentRecord](recordJSON)
		if err != nil {
			return nil, err
		}
		record.CreatedAt = Seq(createdAt)
		if retiredAt.Valid {
			retired := Seq(retiredAt.Int64)
			record.RetiredAt = &retired
		}
		if at.Current {
			if record.RetiredAt == nil {
				return record, nil
			}
			continue
		}
		if isDocumentAliveAt(record, at) {
			copied := *record
			return &copied, nil
		}
		last = record
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = last
	return nil, nil
}

// sqliteLastRevisionTx returns the newest stored revision of a document.
func sqliteLastRevisionTx(ctx context.Context, tx *sql.Tx, id Id) (*DocumentContent, error) {
	var contentJSON string
	err := tx.QueryRowContext(ctx,
		"SELECT content FROM document_revisions WHERE document_id = ? ORDER BY seq DESC LIMIT 1", id).Scan(&contentJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var content DocumentContent
	if err := unmarshalJSONString(contentJSON, &content); err != nil {
		return nil, err
	}
	return &content, nil
}

// sqliteMaterializeDocumentTx materializes one incarnation at a point inside a
// transaction.
func sqliteMaterializeDocumentTx(ctx context.Context, tx *sql.Tx, id Id, at DocumentPoint) (*StoredDocument, error) {
	row, err := sqliteDocumentTx(ctx, tx, id)
	if err != nil || row == nil {
		return nil, err
	}
	if !at.Current && isCurrentOnlyDocument(&row.record) {
		return nil, fmt.Errorf("Document %d does not retain historical content", id)
	}
	if !isDocumentAliveAt(&row.record, at) {
		return nil, nil
	}
	query := "SELECT seq, content FROM document_revisions WHERE document_id = ?"
	args := []any{id}
	if !at.Current {
		query += " AND seq <= ?"
		args = append(args, at.Seq)
	}
	query += " ORDER BY seq"
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	revisions := []storedDocumentRevision{}
	for rows.Next() {
		var seq int64
		var contentJSON string
		if err := rows.Scan(&seq, &contentJSON); err != nil {
			return nil, err
		}
		var content DocumentContent
		if err := unmarshalJSONString(contentJSON, &content); err != nil {
			return nil, err
		}
		revisions = append(revisions, storedDocumentRevision{content: content, seq: Seq(seq)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	baseIndex := len(revisions) - 1
	for baseIndex >= 0 && revisions[baseIndex].content.Kind != ContentBase {
		baseIndex--
	}
	if baseIndex < 0 {
		return nil, fmt.Errorf("Document %d is missing a required base", id)
	}
	base := revisions[baseIndex].content
	batches, err := documentDeltaOps(id, base.Version, revisions, baseIndex+1)
	if err != nil {
		return nil, err
	}
	var materialized any
	if len(base.Value) == 0 {
		materialized = map[string]any{}
	} else if err := unmarshalBytes(base.Value, &materialized); err != nil {
		return nil, err
	}
	for _, ops := range batches {
		materialized, err = delta.ApplyImmutable(materialized, ops)
		if err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(materialized)
	if err != nil {
		return nil, err
	}
	return &StoredDocument{
		Record:          row.record,
		Version:         base.Version,
		Value:           encoded,
		DeltasSinceBase: len(revisions) - baseIndex - 1,
	}, nil
}

// FindDocument returns the incarnation alive at the point for an exact address.
func (s *SqliteStorage) FindDocument(ctx context.Context, address DocumentAddress, at DocumentPoint) (*DocumentRecord, error) {
	if err := s.assertOpen(); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	return sqliteFindDocumentTx(ctx, tx, address, at)
}

// Document materializes one incarnation at the point.
func (s *SqliteStorage) Document(ctx context.Context, id Id, at DocumentPoint) (*StoredDocument, error) {
	if err := s.assertOpen(); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	return sqliteMaterializeDocumentTx(ctx, tx, id, at)
}

// ScanDocuments scans the incarnations alive in one scope at the point.
func (s *SqliteStorage) ScanDocuments(ctx context.Context, query DocumentQuery, cursor Cursor, limit int) (Page[DocumentRecord], error) {
	if err := s.assertOpen(); err != nil {
		return Page[DocumentRecord]{}, err
	}
	after, err := sqliteCursorID(cursor)
	if err != nil {
		return Page[DocumentRecord]{}, err
	}
	conditions := []string{"scope_kind = ?", "owner_id = ?"}
	args := []any{query.Scope.Kind, sqliteDocumentOwner(query.Scope)}
	if query.Kind != nil {
		conditions = append(conditions, "kind = ?")
		args = append(args, *query.Kind)
	}
	if after != nil {
		conditions = append(conditions, "id > ?")
		args = append(args, *after)
	}
	queryBuilder := "SELECT id, record, created_at, retired_at FROM documents WHERE " + joinConditions(conditions) + " ORDER BY id LIMIT ?"
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, queryBuilder, args...)
	if err != nil {
		return Page[DocumentRecord]{}, err
	}
	defer rows.Close()
	values := []DocumentRecord{}
	for rows.Next() {
		var id Id
		var recordJSON string
		var createdAt int64
		var retiredAt sql.NullInt64
		if err := rows.Scan(&id, &recordJSON, &createdAt, &retiredAt); err != nil {
			return Page[DocumentRecord]{}, err
		}
		record, err := unmarshalRecord[DocumentRecord](recordJSON)
		if err != nil {
			return Page[DocumentRecord]{}, err
		}
		record.CreatedAt = Seq(createdAt)
		if retiredAt.Valid {
			retired := Seq(retiredAt.Int64)
			record.RetiredAt = &retired
		}
		if !isDocumentAliveAt(record, query.At) {
			continue
		}
		values = append(values, *record)
		if len(values) > limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return Page[DocumentRecord]{}, err
	}
	return pageOf(values, limit, func(value DocumentRecord) Id { return value.ID }), nil
}

// --- helpers ---

func sqliteDocumentOwner(scope DocumentScope) any {
	switch scope.Kind {
	case ScopeConversation:
		if scope.ConversationID == nil {
			return nil
		}
		return *scope.ConversationID
	case ScopeTask:
		if scope.TaskID == nil {
			return nil
		}
		return *scope.TaskID
	}
	return 0
}

// sqliteKeyValue stores the family key; the singleton key is the empty string
// (the upstream schema declares key_value NOT NULL).
func sqliteKeyValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func sqliteBool(value bool) int {
	if value {
		return 1
	}
	return 0
}

// marshalRecordString encodes a record for the documents.record column.
func marshalRecordString(value any) string {
	encoded, err := marshalJSONValue(value)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func joinConditions(conditions []string) string {
	joined := ""
	for index, condition := range conditions {
		if index > 0 {
			joined += " AND "
		}
		joined += condition
	}
	return joined
}
