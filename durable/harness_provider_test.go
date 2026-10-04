package durable

import (
	"context"
	"testing"

	"github.com/dat267/pier/chord"
)

// providerRuntimeStub adapts a Session to the TaskRuntime operations
// EnsureProviderSessionID uses; the embedded nil interface panics if another
// method is reached.
type providerRuntimeStub struct {
	TaskRuntime
	session        *Session
	conversationID Id
}

func (s providerRuntimeStub) ConversationID() Id { return s.conversationID }

func (s providerRuntimeStub) Snapshot(ctx chord.Context, definition DocDefinition, args ...any) (chord.JsonValue, bool, error) {
	return s.session.Snapshot(ctx, definition, args...)
}

func (s providerRuntimeStub) Commit(change func(tx *Transaction, current RunningTask) (*NextTaskState, error), ctx chord.Context) error {
	return s.session.Commit(ctx, func(tx *Transaction) error {
		_, err := change(tx, RunningTask{})
		return err
	})
}

// TestEnsureProviderSessionIDMigratesLegacyConversation covers the lazy
// backfill: a conversation without pi.provider gets one commit whose Doc runs
// the initializer, and the identity is then stable.
func TestEnsureProviderSessionIDMigratesLegacyConversation(t *testing.T) {
	session, _ := newRootSession(t)
	ctx := context.Background()
	runtime := providerRuntimeStub{session: session, conversationID: RootConversationID}
	if _, present, err := session.Snapshot(ctx, ProviderDoc.Definition, RootConversationID); err != nil || present {
		t.Fatalf("legacy present = %v, %v", present, err)
	}
	first, err := EnsureProviderSessionID(runtime, ctx)
	if err != nil || first == "" {
		t.Fatalf("first = %q, %v", first, err)
	}
	// A UUIDv7 (text form with the version nibble at index 14).
	if len(first) != 36 || first[14] != '7' {
		t.Fatalf("id = %q", first)
	}
	second, err := EnsureProviderSessionID(runtime, ctx)
	if err != nil || second != first {
		t.Fatalf("second = %q, %v", second, err)
	}
	value, present, err := session.Snapshot(ctx, ProviderDoc.Definition, RootConversationID)
	if err != nil || !present {
		t.Fatalf("present = %v, %v", present, err)
	}
	state, ok := decodeJSONInto[ProviderState](value)
	if !ok || state.SessionID != first {
		t.Fatalf("state = %+v", state)
	}
}

// TestHarnessProviderIdentityPerConversation checks every conversation the
// harness creates (or forks) carries its own persisted identity.
func TestHarnessProviderIdentityPerConversation(t *testing.T) {
	harness, _ := openTestHarness(t)
	ctx := context.Background()
	root, err := harness.Root(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	rootRuntime := providerRuntimeStub{session: harness.Session, conversationID: root.ID()}
	rootID, err := EnsureProviderSessionID(rootRuntime, ctx)
	if err != nil || rootID == "" {
		t.Fatalf("root id = %q, %v", rootID, err)
	}
	var entryID Id
	if err := root.Commit(ctx, func(tx *Transaction) error {
		entry, err := tx.AppendEntry(root.ID(), EntryDraft{Kind: "note"})
		entryID = entry.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	forked, err := root.Fork(ctx, entryID, ConversationCreateOptions{
		Ownership: ConversationOwnership{Kind: ConversationOwnerless},
	})
	if err != nil {
		t.Fatal(err)
	}
	forkID, err := EnsureProviderSessionID(providerRuntimeStub{session: harness.Session, conversationID: forked.ID()}, ctx)
	if err != nil || forkID == "" {
		t.Fatalf("fork id = %q, %v", forkID, err)
	}
	if forkID == rootID {
		t.Fatal("a fork must start with a fresh provider identity")
	}
}
