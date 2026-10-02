package durable

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Go-only tests for session/forks.ts: the definition-free document copies one
// fork creates. Upstream exercises them through the harness conversation
// suites; the expectations come from prepareForkDocumentCopies itself.

func createForkDocument(t *testing.T, storage Storage, id Id, kind string, key *string, scope DocumentScope, history, fork string, content any) {
	t.Helper()
	encoded, err := marshalJSONValue(content)
	if err != nil {
		t.Fatal(err)
	}
	historyValue, forkValue := history, fork
	mustCommit(t, storage, StorageWrite{
		Type: "document.create",
		DocumentCreate: &DocumentCreate{
			ID: id, Kind: kind, Key: key, Scope: scope, History: &historyValue, Fork: &forkValue,
		},
		DocumentContent: &DocumentContent{Version: 1, Kind: ContentBase, Value: json.RawMessage(encoded)},
	})
}

func TestPrepareForkDocumentCopies(t *testing.T) {
	documentBackends(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		root, child := RootConversationID, Id(50)
		mustCommit(t, storage, conversationWrite(root), conversationWrite(child))
		rootScope := DocumentScope{Kind: ScopeConversation, ConversationID: &root}
		// The as-of document exists before the fork point.
		createForkDocument(t, storage, mustMintID(t, storage), "notes", nil, rootScope, HistoryRewindable, ForkAsOf,
			map[string]any{"value": float64(1)})
		// Current-only documents exist before the fork point too.
		createForkDocument(t, storage, mustMintID(t, storage), "settings", nil, rootScope, HistoryLatest, ForkCurrent,
			map[string]any{"value": float64(2)})
		key := "member"
		createForkDocument(t, storage, mustMintID(t, storage), "family", &key, rootScope, HistoryRewindable, ForkCurrent,
			map[string]any{"value": float64(3)})
		entryID := mustMintID(t, storage)
		entrySeq := mustCommit(t, storage, entryWrite(entryID, root, nil))
		// A document created after the fork point is not selected as-of.
		later := "later"
		createForkDocument(t, storage, mustMintID(t, storage), "notes", &later, rootScope, HistoryRewindable, ForkAsOf,
			map[string]any{"value": float64(4)})

		copies, err := PrepareForkDocumentCopies(ctx, storage, root, entryID, child)
		if err != nil {
			t.Fatal(err)
		}
		if len(copies) != 3 {
			t.Fatalf("copies = %+v", copies)
		}
		kinds := map[string]int{}
		for _, copy := range copies {
			kinds[copy.Record.Kind]++
			if copy.Record.Scope.Kind != ScopeConversation || copy.Record.Scope.ConversationID == nil ||
				*copy.Record.Scope.ConversationID != child {
				t.Fatalf("record = %+v", copy.Record)
			}
			if copy.Record.History == nil || copy.Record.Fork == nil {
				t.Fatalf("record semantics = %+v", copy.Record)
			}
			if copy.Source.ID == 0 {
				t.Fatalf("source = %+v", copy.Source)
			}
			if copy.Record.Kind == "notes" {
				if copy.Source.At.Current || copy.Source.At.Seq != entrySeq {
					t.Fatalf("as-of source = %+v", copy.Source.At)
				}
				if *copy.Record.Fork != ForkAsOf || *copy.Record.History != HistoryRewindable {
					t.Fatalf("notes semantics = %+v", copy.Record)
				}
			} else if !copy.Source.At.Current {
				t.Fatalf("current source = %+v", copy.Source.At)
			}
		}
		if kinds["notes"] != 1 || kinds["settings"] != 1 || kinds["family"] != 1 {
			t.Fatalf("kinds = %v", kinds)
		}
	})
}

func TestPrepareForkDocumentCopiesRejectsDuplicateAddress(t *testing.T) {
	documentBackends(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		root, child := RootConversationID, Id(50)
		mustCommit(t, storage, conversationWrite(root), conversationWrite(child))
		rootScope := DocumentScope{Kind: ScopeConversation, ConversationID: &root}
		// Two incarnations of the same singleton address: an as-of one before
		// the fork point and its current-only successor after it.
		firstID := mustMintID(t, storage)
		createForkDocument(t, storage, firstID, "notes", nil, rootScope, HistoryRewindable, ForkAsOf,
			map[string]any{"value": float64(1)})
		entryID := mustMintID(t, storage)
		mustCommit(t, storage, entryWrite(entryID, root, nil))
		secondID := mustMintID(t, storage)
		encoded, err := marshalJSONValue(map[string]any{"value": float64(2)})
		if err != nil {
			t.Fatal(err)
		}
		history, fork := HistoryLatest, ForkCurrent
		mustCommit(t, storage,
			StorageWrite{Type: "document.retire", DocumentID: &firstID},
			StorageWrite{
				Type: "document.create",
				DocumentCreate: &DocumentCreate{
					ID: secondID, Kind: "notes", Scope: rootScope, History: &history, Fork: &fork,
				},
				DocumentContent: &DocumentContent{Version: 1, Kind: ContentBase, Value: json.RawMessage(encoded)},
			})
		_, err = PrepareForkDocumentCopies(ctx, storage, root, entryID, child)
		if err == nil || !strings.Contains(err.Error(), "Fork selects multiple source documents for notes") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestPrepareForkDocumentCopiesRequiresVisibleEntry(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()
	mustCommit(t, storage, conversationWrite(RootConversationID))
	_, err := PrepareForkDocumentCopies(ctx, storage, RootConversationID, 999, 50)
	if err == nil || !strings.Contains(err.Error(), "is not visible from conversation") {
		t.Fatalf("err = %v", err)
	}
}
