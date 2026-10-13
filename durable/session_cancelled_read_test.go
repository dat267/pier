package durable

import (
	"context"
	"errors"
	"testing"
)

// Port of harness-storage-failure.test.ts's "does not fail it for a read its
// caller cancelled": a read whose context is already aborted fails only that
// call, not the Session.
type cancelledReadStorage struct {
	Storage
	err error
}

func (s *cancelledReadStorage) FindDocument(ctx context.Context, address DocumentAddress, point DocumentPoint) (*DocumentRecord, error) {
	if ctx.Err() != nil {
		return nil, s.err
	}
	return s.Storage.FindDocument(ctx, address, point)
}

func TestCancelledReadDoesNotFailSession(t *testing.T) {
	cancelErr := errors.New("caller gave up")
	storage := &cancelledReadStorage{Storage: NewMemoryStorage(), err: cancelErr}
	session := NewSession(storage)
	if _, err := storeRoot(t, session); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := session.Snapshot(ctx, InboxDoc.Definition, RootConversationID); !errors.Is(err, cancelErr) {
		t.Fatalf("snapshot = %v, want the cancellation", err)
	}
	if session.Failure() != nil {
		t.Fatalf("a cancelled read failed the Session: %v", session.Failure())
	}
	if err := session.Commit(context.Background(), func(tx *Transaction) error {
		_, err := tx.AppendEntry(RootConversationID, EntryDraft{Kind: "note"})
		return err
	}); err != nil {
		t.Fatalf("commit after a cancelled read = %v", err)
	}
}
