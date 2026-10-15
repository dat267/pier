package durable

import (
	"context"
	"testing"
)

// Upstream v1.3.0 #10937 validateSqliteSchemaPrefix.
func TestValidateSQLiteSchemaPrefix(t *testing.T) {
	for _, valid := range []string{"pi_", "_host", "A1", "schema2"} {
		if err := ValidateSQLiteSchemaPrefix(valid); err != nil {
			t.Errorf("ValidateSQLiteSchemaPrefix(%q) = %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "1bad", "has space", "dash-", "dot.name"} {
		if err := ValidateSQLiteSchemaPrefix(invalid); err == nil {
			t.Errorf("ValidateSQLiteSchemaPrefix(%q) accepted an invalid prefix", invalid)
		}
	}
	for _, reserved := range []string{"sqlite_", "sqlite_x", "SQLite_extra"} {
		if err := ValidateSQLiteSchemaPrefix(reserved); err == nil {
			t.Errorf("ValidateSQLiteSchemaPrefix(%q) accepted a reserved prefix", reserved)
		}
	}
}

// Upstream v1.3.0 #10937: two storages with different prefixes share one
// database, each with its own tables.
func TestSqliteStorageNamespaceCoexists(t *testing.T) {
	path := t.TempDir() + "/shared.db"
	ctx := context.Background()
	open := func(prefix string) *SqliteStorage {
		storage, err := OpenSqliteStorageWithOptions(path, SqliteStorageOptions{Prefix: prefix})
		if err != nil {
			t.Fatalf("open %q: %v", prefix, err)
		}
		return storage
	}

	pi := open("pi_")
	mustCommit(t, pi, conversationWrite(2))
	if err := pi.Close(ctx); err != nil {
		t.Fatal(err)
	}

	host := open("host_")
	// A host table lives beside the namespaced durable schemas.
	if _, err := host.db.db.Exec("CREATE TABLE host_notes (text TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatal(err)
	}

	pi = open("pi_")
	defer pi.Close(ctx)
	if record, err := pi.Conversation(ctx, 2); err != nil || record == nil {
		t.Fatalf("pi conversation = %v, %v", record, err)
	}
	host = open("host_")
	defer host.Close(ctx)
	if record, err := host.Conversation(ctx, 2); err != nil || record != nil {
		t.Fatalf("host conversation = %v, %v; want none", record, err)
	}
}

// The prefix reaches the initial schema: the base names must not be created.
func TestSqliteStorageNamespaceRenamesTables(t *testing.T) {
	path := t.TempDir() + "/ns.db"
	storage, err := OpenSqliteStorageWithOptions(path, SqliteStorageOptions{Prefix: "pi_"})
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close(context.Background())

	countTable := func(name string) int {
		var count int
		if err := storage.db.db.QueryRow(
			`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if got := countTable("pi_conversations"); got != 1 {
		t.Fatalf("pi_conversations count = %d", got)
	}
	if got := countTable("conversations"); got != 0 {
		t.Fatalf("base conversations table exists: %d", got)
	}
	// Indexes are namespaced too.
	var index string
	if err := storage.db.db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'pi_conversations_by_owner_conversation'`).Scan(&index); err != nil {
		t.Fatalf("prefixed index missing: %v", err)
	}
}

// An invalid prefix is rejected before any statement runs.
func TestSqliteStorageRejectsInvalidPrefix(t *testing.T) {
	if _, err := OpenSqliteStorageWithOptions(t.TempDir()+"/bad.db", SqliteStorageOptions{Prefix: "sqlite_x"}); err == nil {
		t.Fatal("reserved prefix was accepted")
	}
}
