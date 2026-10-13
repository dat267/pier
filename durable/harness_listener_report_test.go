package durable

import (
	"context"
	"testing"

	"github.com/dat267/pier/chord"
)

// Port of harness/harness.ts's safeReport/safeNow and session.ts's listener
// containment (v1.2.0): a throwing host listener is reported through onReport
// instead of failing the operation that ran it, while the Session's own
// listeners fail the Session; a throwing clock is reported once and falls back
// to the wall clock.

func TestSafeReportContainsThrowingHandler(t *testing.T) {
	report := safeReport(func(error) { panic("onReport failed") })
	report(nil) // must not panic
}

func TestSafeNowReportsOnceAndFallsBack(t *testing.T) {
	reports := 0
	now := safeNow(func() int64 { panic("clock failed") }, func(error) { reports++ })
	first := now()
	second := now()
	if first <= 0 || second <= 0 {
		t.Fatalf("fallback clock = %d, %d, want wall-clock values", first, second)
	}
	if reports != 1 {
		t.Fatalf("reports = %d, want 1", reports)
	}
}

func TestCommitListenerPanicIsReportedNotPropagated(t *testing.T) {
	storage := NewMemoryStorage()
	var reported []error
	harness := NewHarness(storage, HarnessOptions{OnReport: func(err error) { reported = append(reported, err) }}, context.Background())
	session := harness.Session
	t.Cleanup(func() { _ = session.Close(context.Background()) })

	ran := 0
	session.SubscribeCommits(func(CommitPublication, chord.Context) { panic("listener failed") })
	session.SubscribeCommits(func(CommitPublication, chord.Context) { ran++ })
	if err := session.Commit(context.Background(), func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatalf("commit with throwing host listener = %v", err)
	}
	if ran != 1 {
		t.Fatalf("later host listener ran %d times, want 1", ran)
	}
	if len(reported) != 1 {
		t.Fatalf("reported = %v, want the one listener panic", reported)
	}
}

func TestCloseListenerPanicIsReportedNotPropagated(t *testing.T) {
	storage := NewMemoryStorage()
	var reported []error
	harness := NewHarness(storage, HarnessOptions{OnReport: func(err error) { reported = append(reported, err) }}, context.Background())
	session := harness.Session

	ran := 0
	session.SubscribeClose(func() { panic("close listener failed") })
	session.SubscribeClose(func() { ran++ })
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("close with throwing listener = %v", err)
	}
	if ran != 1 {
		t.Fatalf("later close listener ran %d times, want 1", ran)
	}
	if len(reported) != 1 {
		t.Fatalf("reported = %v, want the one close-listener panic", reported)
	}
}

func TestInternalCommitListenerPanicFailsSession(t *testing.T) {
	session, _ := newRootSession(t)
	session.ObserveCommits(func(CommitPublication, chord.Context) { panic("internal listener failed") })
	// The commit is already durable when listeners run, so it succeeds; the
	// Session fails afterward, as an internal listener's throw is final.
	if err := session.Commit(context.Background(), func(tx *Transaction) error {
		_, err := tx.AppendEntry(RootConversationID, EntryDraft{Kind: "note"})
		return err
	}); err != nil {
		t.Fatalf("commit with throwing internal listener = %v", err)
	}
	if session.Failure() == nil {
		t.Fatal("the Session did not fail after its internal listener threw")
	}
}
