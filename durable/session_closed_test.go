package durable

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Port of session.ts's `closed` (v1.2.0): it settles once the Session has
// closed, Storage included, with `closed` after a close and `failed` (carrying
// the cause) after a storage error.

func TestSessionClosedSettlesClosed(t *testing.T) {
	session, _ := newRootSession(t)
	select {
	case <-session.Closed():
		t.Fatal("closed settled before Close")
	default:
	}
	if _, ok := session.End(); ok {
		t.Fatal("End reported a result before Close")
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-session.Closed():
	default:
		t.Fatal("closed did not settle after Close")
	}
	end, ok := session.End()
	if !ok || end.Reason != SessionReasonClosed || end.Err != nil {
		t.Fatalf("end = %+v, ok = %v", end, ok)
	}
}

func TestSessionClosedSettlesFailedAfterStorageError(t *testing.T) {
	readErr := errors.New("document disk failure")
	storage := &failingDocumentReadStorage{Storage: NewMemoryStorage(), readErr: readErr, closed: make(chan struct{})}
	session := NewSession(storage)
	if _, err := storeRoot(t, session); err != nil {
		t.Fatal(err)
	}
	if _, _, err := session.Snapshot(context.Background(), InboxDoc.Definition, RootConversationID); !errors.Is(err, readErr) {
		t.Fatalf("snapshot error = %v", err)
	}
	select {
	case <-session.Closed():
	case <-time.After(time.Second):
		t.Fatal("closed did not settle after a storage failure")
	}
	end, ok := session.End()
	if !ok || end.Reason != SessionReasonFailed || !errors.Is(end.Err, readErr) {
		t.Fatalf("end = %+v, ok = %v", end, ok)
	}
}
