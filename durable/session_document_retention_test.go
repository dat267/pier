package durable

import (
	"context"
	"testing"
)

// Port of session-document-retention.test.ts (v1.2.0): the Session's document
// cache is bounded by use. An unobserved loaded document leaves after its
// retention and loads from Storage again; an observed one stays.

func (s *Session) cachedDocumentCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.documents)
}

func openRetentionSession(t *testing.T, retentionMs int) (*Session, *int64) {
	t.Helper()
	storage := NewMemoryStorage()
	now := new(int64)
	*now = 1_000
	session := NewSessionWithClock(storage, func() int64 { return *now })
	session.documentRetention = func() int { return retentionMs }
	t.Cleanup(func() { _ = session.Close(context.Background()) })
	return session, now
}

func TestDocumentCacheEvictsAfterRetention(t *testing.T) {
	session, now := openRetentionSession(t, 60_000)
	ctx := context.Background()
	definition := sessionDocDefinition()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		draft, err := tx.Doc(definition)
		if err != nil {
			return err
		}
		draft.(map[string]any)["count"] = float64(3)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if session.cachedDocumentCount() == 0 {
		t.Fatal("the document was not cached")
	}
	// Inside the retention window a commit keeps it.
	*now += 59_000
	if err := session.Commit(ctx, func(*Transaction) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if session.cachedDocumentCount() == 0 {
		t.Fatal("the document was evicted inside its retention window")
	}
	// Past it, the next commit evicts it.
	*now += 61_000
	if err := session.Commit(ctx, func(*Transaction) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if session.cachedDocumentCount() != 0 {
		t.Fatalf("the document survived its retention: %d cached", session.cachedDocumentCount())
	}
	// A read loads it again with the same value.
	value, ok, err := session.Snapshot(ctx, definition)
	if err != nil || !ok || value.(map[string]any)["count"] != float64(3) {
		t.Fatalf("reload = %+v, %v, %v", value, ok, err)
	}
}

func TestDocumentCacheEvictsWithZeroRetention(t *testing.T) {
	session, _ := openRetentionSession(t, 0)
	ctx := context.Background()
	definition := sessionDocDefinition()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.Doc(definition)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Retention 0 drops the document at the end of the commit that used it.
	if session.cachedDocumentCount() != 0 {
		t.Fatalf("retention 0 kept %d documents", session.cachedDocumentCount())
	}
	// A read reloads it; the line then drops it again.
	if _, _, err := session.Snapshot(ctx, definition); err != nil {
		t.Fatal(err)
	}
	if session.cachedDocumentCount() == 0 {
		t.Fatal("the snapshot did not cache the document")
	}
	if err := session.ReadOnLine(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if session.cachedDocumentCount() != 0 {
		t.Fatalf("retention 0 kept %d documents after the line", session.cachedDocumentCount())
	}
}

func TestDocumentCacheKeepsObservedDocuments(t *testing.T) {
	session, now := openRetentionSession(t, 0)
	ctx := context.Background()
	definition := sessionDocDefinition()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.Doc(definition)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	state, ok, err := session.DocumentState(ctx, definition)
	if err != nil || !ok {
		t.Fatalf("state = %v, %v", ok, err)
	}
	if value, _ := state.Value(); value.(map[string]any)["count"] != float64(0) {
		t.Fatalf("state value = %+v", value)
	}
	*now += 1_000_000
	if err := session.Commit(ctx, func(*Transaction) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if session.cachedDocumentCount() == 0 {
		t.Fatal("an observed document was evicted")
	}
}
