package durable

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// Port of storage/sqlite/schema.ts and storage.ts's schema threading.
//
// D222: upstream renders the prefixed table and index names into every query
// (createSqliteSchema + template strings). The port instead keeps the base
// names in the queries and rewrites them at the database boundary through
// sqliteDB/sqliteTx, so linting and test fixtures see the same SQL either way.
// The statements issued and the resulting schema are identical; only the
// rendering point differs. See docs/DIVERGENCES.md.

// sqliteTableNames are the base table names (upstream TABLE_NAMES). Never
// change an existing value; adding one is fine.
var sqliteTableNames = []string{
	"durable_schema",
	"durable_metadata",
	"record_ids",
	"conversations",
	"entries",
	"tasks",
	"submissions",
	"documents",
	"document_revisions",
}

// sqliteIndexNames are the base index names (upstream INDEX_NAMES).
var sqliteIndexNames = []string{
	"conversations_by_owner_conversation",
	"conversations_by_owner_task",
	"entries_by_conversation",
	"entry_heads_by_conversation",
	"tasks_by_status",
	"tasks_by_conversation",
	"tasks_by_kind",
	"tasks_by_abort_requested",
	"tasks_by_background",
	"submissions_by_request",
	"submissions_by_conversation",
	"submissions_by_status",
	"documents_by_address",
	"documents_by_scope",
	"documents_by_scope_kind",
	"document_revisions_by_kind",
}

var sqliteSchemaPrefixPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidateSQLiteSchemaPrefix validates a prefix before it is used as part of a
// SQLite identifier (upstream validateSqliteSchemaPrefix).
func ValidateSQLiteSchemaPrefix(prefix string) error {
	if !sqliteSchemaPrefixPattern.MatchString(prefix) {
		return fmt.Errorf("Invalid SQLite schema prefix %q", prefix)
	}
	if strings.HasPrefix(strings.ToLower(prefix), "sqlite_") {
		return fmt.Errorf("SQLite schema prefix %q is reserved", prefix)
	}
	return nil
}

// sqliteSchema rewrites the base table and index names to their prefixed forms.
type sqliteSchema struct {
	prefix   string
	replacer *strings.Replacer
}

// createSQLiteSchema builds the prefix rewriter. The replacer matches the
// longest known name, so an index name that starts with a table name prefixes
// as a whole.
func createSQLiteSchema(prefix string) (*sqliteSchema, error) {
	if prefix != "" {
		if err := ValidateSQLiteSchemaPrefix(prefix); err != nil {
			return nil, err
		}
	}
	names := make([]string, 0, len(sqliteTableNames)+len(sqliteIndexNames))
	names = append(names, sqliteTableNames...)
	names = append(names, sqliteIndexNames...)
	replacements := make([]string, 0, 2*len(names))
	for _, name := range names {
		replacements = append(replacements, name, prefix+name)
	}
	return &sqliteSchema{prefix: prefix, replacer: strings.NewReplacer(replacements...)}, nil
}

// apply prefixes every known table and index name in a SQL statement.
func (s *sqliteSchema) apply(query string) string {
	if s == nil || s.prefix == "" {
		return query
	}
	return s.replacer.Replace(query)
}

// sqliteDB wraps *sql.DB and rewrites every statement with the schema prefix.
type sqliteDB struct {
	db     *sql.DB
	schema *sqliteSchema
}

func (d *sqliteDB) Begin() (*sqliteTx, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return nil, err
	}
	return &sqliteTx{tx: tx, schema: d.schema}, nil
}

func (d *sqliteDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sqliteTx, error) {
	tx, err := d.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &sqliteTx{tx: tx, schema: d.schema}, nil
}

func (d *sqliteDB) Close() error { return d.db.Close() }

func (d *sqliteDB) Ping() error { return d.db.Ping() }

func (d *sqliteDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.db.QueryContext(ctx, d.schema.apply(query), args...)
}

func (d *sqliteDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.db.QueryRowContext(ctx, d.schema.apply(query), args...)
}

// sqliteTx wraps *sql.Tx and rewrites every statement with the schema prefix.
type sqliteTx struct {
	tx     *sql.Tx
	schema *sqliteSchema
}

func (t *sqliteTx) Exec(query string, args ...any) (sql.Result, error) {
	return t.tx.Exec(t.schema.apply(query), args...)
}

func (t *sqliteTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, t.schema.apply(query), args...)
}

func (t *sqliteTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, t.schema.apply(query), args...)
}

func (t *sqliteTx) QueryRow(query string, args ...any) *sql.Row {
	return t.tx.QueryRow(t.schema.apply(query), args...)
}

func (t *sqliteTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, t.schema.apply(query), args...)
}

func (t *sqliteTx) Commit() error { return t.tx.Commit() }

func (t *sqliteTx) Rollback() error { return t.tx.Rollback() }
