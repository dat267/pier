package durable

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/dat267/pier/chord"
)

// Go-only tests for session/session.ts: one mutation line, the loaded tracker
// cache, committed publication and observer attachment. Upstream exercises the
// kernel through the harness suites; the expectations come from the session
// kernel itself.

func TestSessionCommitPublishesAndCaches(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	var mu sync.Mutex
	var publications []CommitPublication
	session.SubscribeCommits(func(publication CommitPublication, _ chord.Context) {
		mu.Lock()
		publications = append(publications, publication)
		mu.Unlock()
	})
	err := session.Commit(context.Background(), func(tx *Transaction) error {
		if _, err := tx.CreateRootConversation(); err != nil {
			return err
		}
		if _, err := tx.AppendEntry(RootConversationID, EntryDraft{Kind: "message"}); err != nil {
			return err
		}
		definition := sessionDocDefinition()
		draft, err := tx.Doc(definition)
		if err != nil {
			return err
		}
		draft.(map[string]any)["count"] = float64(7)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	count := len(publications)
	mu.Unlock()
	if count != 1 {
		t.Fatalf("publications = %d", count)
	}
	mu.Lock()
	changes := publications[0].Changes
	mu.Unlock()
	var conversation, entry, document bool
	for _, change := range changes {
		if change.Write != nil {
			switch change.Write.Type {
			case "conversation":
				conversation = true
			case "entry":
				entry = true
			}
		}
		if change.Document != nil && change.Document.Type == "document" {
			document = true
		}
	}
	if !conversation || !entry || !document {
		t.Fatalf("changes = %+v", changes)
	}
	// The value is observable through Snapshot and the state watcher.
	definition := sessionDocDefinition()
	value, ok, err := session.Snapshot(context.Background(), definition)
	if err != nil || !ok || value.(map[string]any)["count"] != float64(7) {
		t.Fatalf("snapshot = %+v, %v, %v", value, ok, err)
	}
	state, ok, err := session.DocumentState(context.Background(), definition)
	if err != nil || !ok {
		t.Fatalf("state = %v, %v", ok, err)
	}
	current, _ := state.Value()
	if current.(map[string]any)["count"] != float64(7) {
		t.Fatalf("state value = %+v", current)
	}
}

func TestSessionDocumentStateFollowsCommits(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	definition := sessionDocDefinition()
	if err := session.Commit(context.Background(), func(tx *Transaction) error {
		if _, err := tx.CreateRootConversation(); err != nil {
			return err
		}
		if _, err := tx.Doc(definition); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	state, ok, err := session.DocumentState(context.Background(), definition)
	if err != nil || !ok {
		t.Fatalf("state = %v, %v", ok, err)
	}
	var mu sync.Mutex
	var updates []int
	state.Subscribe(func(value chord.JsonValue, _ chord.Context, delivery chord.ReplicatedStateDelivery) {
		if delivery.Kind != chord.DeliveryUpdate {
			return
		}
		mu.Lock()
		updates = append(updates, int(value.(map[string]any)["count"].(float64)))
		mu.Unlock()
	})
	if err := session.Commit(context.Background(), func(tx *Transaction) error {
		draft, err := tx.Doc(definition)
		if err != nil {
			return err
		}
		draft.(map[string]any)["count"] = float64(2)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]int(nil), updates...)
	mu.Unlock()
	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("updates = %v", got)
	}
}

func TestSessionSnapshotAsOf(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	root := RootConversationID
	definition := DocDefinition{
		Kind: "notes", Version: 1, Scope: ScopeConversation, History: strPtr(HistoryRewindable), Fork: strPtr(ForkAsOf),
		Initial: func(seed chord.JsonValue) (chord.JsonValue, error) { return map[string]any{"count": float64(0)}, nil },
	}
	var entryID Id
	if err := session.Commit(context.Background(), func(tx *Transaction) error {
		if _, err := tx.CreateRootConversation(); err != nil {
			return err
		}
		draft, err := tx.Doc(definition, root)
		if err != nil {
			return err
		}
		draft.(map[string]any)["count"] = float64(1)
		entry, err := tx.AppendEntry(root, EntryDraft{Kind: "message"})
		if err != nil {
			return err
		}
		entryID = entry.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(context.Background(), func(tx *Transaction) error {
		draft, err := tx.Doc(definition, root)
		if err != nil {
			return err
		}
		draft.(map[string]any)["count"] = float64(2)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The as-of read sees the first commit's value.
	value, ok, err := session.SnapshotAsOf(context.Background(), definition, root, entryID)
	if err != nil || !ok || value.(map[string]any)["count"] != float64(1) {
		t.Fatalf("as-of = %+v, %v, %v", value, ok, err)
	}
	// The current read sees the newest value.
	current, _, err := session.Snapshot(context.Background(), definition, root)
	if err != nil || current.(map[string]any)["count"] != float64(2) {
		t.Fatalf("current = %+v, %v", current, err)
	}
}

func TestSessionCloseSealsAndClosesStorage(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	closed := false
	session.SubscribeClose(func() { closed = true })
	if _, err := storeRoot(t, session); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("close listener did not run")
	}
	if err := session.Commit(context.Background(), func(tx *Transaction) error { return nil }); err == nil ||
		!strings.Contains(err.Error(), "closed") {
		t.Fatalf("commit after close = %v", err)
	}
	if _, err := storage.Conversation(context.Background(), RootConversationID); err == nil {
		t.Fatal("storage stayed open")
	}
	// Close is idempotent.
	if err := session.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// failingStorage poisons a commit after storage admission.
type failingStorage struct {
	Storage
	fail error
}

func (f *failingStorage) Commit(ctx context.Context, writes []StorageWrite) (Seq, error) {
	if f.fail != nil {
		return 0, f.fail
	}
	return f.Storage.Commit(ctx, writes)
}

func TestSessionPoisonsAfterAdmissionFailure(t *testing.T) {
	inner := NewMemoryStorage()
	storage := &failingStorage{Storage: inner}
	session := NewSession(storage)
	if _, err := storeRoot(t, session); err != nil {
		t.Fatal(err)
	}
	storage.fail = errors.New("disk on fire")
	err := session.Commit(context.Background(), func(tx *Transaction) error {
		_, err := tx.AppendEntry(RootConversationID, EntryDraft{Kind: "message"})
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "disk on fire") {
		t.Fatalf("commit = %v", err)
	}
	if err := session.Commit(context.Background(), func(tx *Transaction) error { return nil }); err == nil ||
		!strings.Contains(err.Error(), "poisoned") {
		t.Fatalf("commit after poison = %v", err)
	}
}

// storeRoot creates the root conversation through the session.
func storeRoot(t *testing.T, session *Session) (Id, error) {
	t.Helper()
	var id Id
	err := session.Commit(context.Background(), func(tx *Transaction) error {
		record, err := tx.CreateRootConversation()
		if err != nil {
			return err
		}
		id = record.ID
		return nil
	})
	return id, err
}
