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

type failingCloseStorage struct {
	Storage
	closeErr error
}

func (s *failingCloseStorage) Close(context.Context) error { return s.closeErr }

// A Storage that cannot close is a failed one; `closed` still settles.
func TestSessionCloseFailureSettlesFailed(t *testing.T) {
	closeErr := errors.New("backend close failed")
	storage := &failingCloseStorage{Storage: NewMemoryStorage(), closeErr: closeErr}
	session := NewSession(storage)
	if _, err := storeRoot(t, session); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(context.Background()); !errors.Is(err, closeErr) {
		t.Fatalf("close = %v", err)
	}
	end, ok := session.End()
	if !ok || end.Reason != SessionReasonFailed || !errors.Is(end.Err, closeErr) {
		t.Fatalf("end = %+v, ok = %v", end, ok)
	}
}

type failingReadAndCloseStorage struct {
	Storage
	readErr  error
	closeErr error
}

func (s *failingReadAndCloseStorage) FindDocument(context.Context, DocumentAddress, DocumentPoint) (*DocumentRecord, error) {
	return nil, s.readErr
}

func (s *failingReadAndCloseStorage) Close(context.Context) error { return s.closeErr }

// An earlier failure stays the cause when closing also fails.
func TestSessionClosedKeepsTheFirstFailure(t *testing.T) {
	readErr := errors.New("read failed")
	closeErr := errors.New("close failed")
	storage := &failingReadAndCloseStorage{Storage: NewMemoryStorage(), readErr: readErr, closeErr: closeErr}
	session := NewSession(storage)
	if _, err := storeRoot(t, session); err != nil {
		t.Fatal(err)
	}
	if _, _, err := session.Snapshot(context.Background(), InboxDoc.Definition, RootConversationID); !errors.Is(err, readErr) {
		t.Fatalf("snapshot = %v", err)
	}
	select {
	case <-session.Closed():
	case <-time.After(time.Second):
		t.Fatal("closed did not settle")
	}
	end, ok := session.End()
	if !ok || end.Reason != SessionReasonFailed || !errors.Is(end.Err, readErr) {
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
