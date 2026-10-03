package durable

import (
	"context"
	"testing"

	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
)

// Port of harness/view.ts.

type recordingObserver struct {
	initial      ConversationView
	advances     []ConversationView
	ops          [][]delta.Op
	publications int
	closed       bool
}

func (o *recordingObserver) CloseSession() { o.closed = true }

func (o *recordingObserver) Advance(value ConversationView, ops []delta.Op, ctx chord.Context) {
	o.advances = append(o.advances, value)
	o.ops = append(o.ops, ops)
}

func (o *recordingObserver) Publication(before, after ConversationView, ops []delta.Op, publication CommitPublication, ctx chord.Context) {
	o.publications++
}

func TestConversationViewsBuildAndAdvance(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		if _, err := tx.CreateRootConversation(); err != nil {
			return err
		}
		if _, err := tx.AppendEntry(RootConversationID, EntryDraft{Kind: "message"}); err != nil {
			return err
		}
		return Configure(tx, RootConversationID, AgentChange{Instructions: SetOf("first")})
	}); err != nil {
		t.Fatal(err)
	}
	views := NewConversationViews(session, storage)
	observer := &recordingObserver{}
	_, detach, err := views.Attach(RootConversationID, func(value ConversationView, release func(), _ Storage) (ViewObserver, error) {
		observer.initial = value
		return observer, nil
	}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	if observer.initial.Conversation.ID != RootConversationID || len(observer.initial.Entries) != 1 {
		t.Fatalf("initial = %+v", observer.initial)
	}
	tools, present := observer.initial.Docs["pi.agent"]
	if !present || tools == nil {
		t.Fatalf("agent doc = %+v", observer.initial.Docs)
	}
	// A commit with a new entry and a document change advances the mount.
	if err := session.Commit(ctx, func(tx *Transaction) error {
		if _, err := tx.AppendEntry(RootConversationID, EntryDraft{Kind: "message"}); err != nil {
			return err
		}
		return Configure(tx, RootConversationID, AgentChange{Instructions: SetOf("second")})
	}); err != nil {
		t.Fatal(err)
	}
	if len(observer.advances) == 0 {
		t.Fatal("no advance")
	}
	last := observer.advances[len(observer.advances)-1]
	if len(last.Entries) != 2 {
		t.Fatalf("entries = %+v", last.Entries)
	}
	agent := last.Docs["pi.agent"].(map[string]any)
	if agent["instructions"] != "second" {
		t.Fatalf("agent = %#v", agent)
	}
	if len(observer.ops[len(observer.ops)-1]) == 0 {
		t.Fatal("advance carried no ops")
	}
	if observer.publications == 0 {
		t.Fatal("no publication")
	}
}

func TestConversationViewsHeadMarkerKeepsSuffix(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var first Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		entry, err := tx.AppendEntry(RootConversationID, EntryDraft{Kind: "message"})
		if err != nil {
			return err
		}
		first = entry.ID
		_, err = tx.AppendEntry(RootConversationID, EntryDraft{Kind: "message"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	views := NewConversationViews(session, storage)
	observer := &recordingObserver{}
	_, detach, err := views.Attach(RootConversationID, func(value ConversationView, release func(), _ Storage) (ViewObserver, error) {
		return observer, nil
	}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	// A head marker at the first entry keeps the second entry after it.
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.AppendEntry(RootConversationID, EntryDraft{Kind: "pi.reset", Head: &first})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	last := observer.advances[len(observer.advances)-1]
	// The marker goes in front and keeps every non-head entry from its head on.
	if len(last.Entries) != 3 || last.Entries[0].Kind != "pi.reset" ||
		last.Entries[1].ID != first || last.Entries[2].ID != first+1 {
		t.Fatalf("entries = %+v", last.Entries)
	}
}

func TestConversationViewsStateDeliversFrames(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	views := NewConversationViews(session, storage)
	state, err := views.State(RootConversationID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	var seen []ConversationView
	unsubscribe := state.Subscribe(func(value ConversationView, ctx chord.Context, delivery chord.ReplicatedStateDelivery) {
		seen = append(seen, value)
	})
	defer unsubscribe()
	if len(seen) == 0 {
		t.Fatal("no hydration")
	}
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.AppendEntry(RootConversationID, EntryDraft{Kind: "message"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(seen) < 2 || len(seen[len(seen)-1].Entries) != 1 {
		t.Fatalf("seen = %+v", seen)
	}
}

func TestConversationViewsWatchCancels(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	views := NewConversationViews(session, storage)
	watchCtx, cancel := context.WithCancel(ctx)
	watch, err := views.Watch(RootConversationID, watchCtx)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	<-watch.Closed()
	end, ok := watch.End()
	if !ok || end.Reason != WatchReasonCancelled {
		t.Fatalf("end = %+v, %v", end, ok)
	}
}

func TestConversationViewsForNonHarness(t *testing.T) {
	if _, err := ConversationViewsFor(NewSession(NewMemoryStorage())); err == nil {
		t.Fatal("a bare session must not be a harness")
	}
}
