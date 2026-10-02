package durable

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/chord/delta"
)

// Port of the document cases of upstream
// packages/durable/src/testing/storage-conformance.ts, run against every
// backend (memory, sqlite, jsonl).

// documentBackends runs one test against all backends.
func documentBackends(t *testing.T, run func(t *testing.T, storage Storage)) {
	t.Helper()
	t.Run("memory", func(t *testing.T) { run(t, NewMemoryStorage()) })
	t.Run("sqlite", func(t *testing.T) { run(t, newSqliteStorageConformance(t)) })
	t.Run("jsonl", func(t *testing.T) { run(t, newJsonlStorageConformance(t)) })
}

func mustMintID(t *testing.T, storage Storage) Id {
	t.Helper()
	id, err := storage.MintID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func documentValue(t *testing.T, stored *StoredDocument) map[string]any {
	t.Helper()
	var value map[string]any
	if stored == nil {
		t.Fatal("missing document")
	}
	if err := json.Unmarshal(stored.Value, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

// TestDocumentLifecycleReconstructsRewindable covers upstream's
// "reconstructs rewindable documents and preserves half-open incarnations".
func TestDocumentLifecycleReconstructsRewindable(t *testing.T) {
	documentBackends(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		rootID := RootConversationID
		mustCommit(t, storage, conversationWrite(rootID))
		firstID := mustMintID(t, storage)
		history, fork := HistoryRewindable, ForkAsOf
		firstRecord := &DocumentCreate{
			ID: firstID, Kind: "conversation.notes",
			Scope: DocumentScope{Kind: ScopeConversation, ConversationID: &rootID, History: &history, Fork: &fork},
		}
		createdAt := mustCommit(t, storage, StorageWrite{
			Type: "document.create", DocumentCreate: firstRecord,
			DocumentContent: &DocumentContent{Version: 1, Kind: ContentBase,
				Value: json.RawMessage(`{"items":["a"],"nested":{"count":1}}`)},
		})
		changedAt := mustCommit(t, storage, StorageWrite{
			Type: "document.change", DocumentID: &firstID,
			DocumentContent: &DocumentContent{Version: 1, Kind: ContentDelta, Ops: []any{
				[]any{"p", []any{"items"}, 1, 0, []any{"b"}},
				[]any{"s", []any{"nested", "count"}, 2},
			}},
		})

		// The created point reads the base; the changed point replays the delta.
		atCreated, err := storage.Document(ctx, firstID, DocumentPointAt(createdAt))
		if err != nil {
			t.Fatal(err)
		}
		if atCreated == nil || atCreated.DeltasSinceBase != 0 || documentValue(t, atCreated)["nested"].(map[string]any)["count"].(float64) != 1 {
			t.Fatalf("created = %+v", atCreated)
		}
		atChanged, err := storage.Document(ctx, firstID, DocumentPointAt(changedAt))
		if err != nil {
			t.Fatal(err)
		}
		value := documentValue(t, atChanged)
		items := value["items"].([]any)
		if len(items) != 2 || items[0] != "a" || items[1] != "b" ||
			value["nested"].(map[string]any)["count"].(float64) != 2 || atChanged.DeltasSinceBase != 1 {
			t.Fatalf("changed = %s (%+v)", atChanged.Value, atChanged)
		}
		// A returned value is detached.
		value["items"].([]any)[0] = "read mutation"
		current, err := storage.Document(ctx, firstID, CurrentDocumentPoint())
		if err != nil {
			t.Fatal(err)
		}
		if items := documentValue(t, current)["items"].([]any); items[0] != "a" {
			t.Fatalf("current aliased the reader: %v", items)
		}

		// A checkpoint (new base version) and a delta after it.
		checkpointAt := mustCommit(t, storage, StorageWrite{
			Type: "document.change", DocumentID: &firstID,
			DocumentContent: &DocumentContent{Version: 2, Kind: ContentBase,
				Value: json.RawMessage(`{"items":["checkpoint"],"nested":{"count":3}}`)},
		})
		replacedAt := mustCommit(t, storage, StorageWrite{
			Type: "document.change", DocumentID: &firstID,
			DocumentContent: &DocumentContent{Version: 2, Kind: ContentDelta, Ops: []any{
				[]any{"r", map[string]any{"items": []any{"replacement"}, "nested": map[string]any{"count": 4}}},
			}},
		})
		// Historical points still read the old versions.
		historical, _ := storage.Document(ctx, firstID, DocumentPointAt(changedAt))
		if historical.Version != 1 || documentValue(t, historical)["items"].([]any)[0] != "a" {
			t.Fatalf("historical = %s", historical.Value)
		}
		atCheckpoint, _ := storage.Document(ctx, firstID, DocumentPointAt(checkpointAt))
		if atCheckpoint.Version != 2 || atCheckpoint.DeltasSinceBase != 0 {
			t.Fatalf("checkpoint = %+v", atCheckpoint)
		}
		atReplaced, _ := storage.Document(ctx, firstID, DocumentPointAt(replacedAt))
		if atReplaced.DeltasSinceBase != 1 || documentValue(t, atReplaced)["items"].([]any)[0] != "replacement" {
			t.Fatalf("replaced = %s (%+v)", atReplaced.Value, atReplaced)
		}
		current, _ = storage.Document(ctx, firstID, CurrentDocumentPoint())
		if current.DeltasSinceBase != 1 {
			t.Fatalf("current deltas = %d", current.DeltasSinceBase)
		}

		// A second incarnation replaces the first at the same address.
		secondID := mustMintID(t, storage)
		retiredAt := mustCommit(t, storage,
			StorageWrite{Type: "document.create", DocumentCreate: &DocumentCreate{
				ID: secondID, Kind: "conversation.notes",
				Scope: DocumentScope{Kind: ScopeConversation, ConversationID: &rootID, History: &history, Fork: &fork},
			}, DocumentContent: &DocumentContent{Version: 1, Kind: ContentBase, Value: json.RawMessage(`{"items":["new"]}`)}},
			StorageWrite{Type: "document.retire", DocumentID: &firstID},
			StorageWrite{Type: "document.change", DocumentID: &firstID, DocumentContent: &DocumentContent{
				Version: 2, Kind: ContentDelta, Ops: []any{[]any{"s", []any{"retiring"}, true}},
			}},
		)
		address := DocumentAddress{Kind: "conversation.notes", Scope: DocumentScope{Kind: ScopeConversation, ConversationID: &rootID}}
		atOld, err := storage.FindDocument(ctx, address, DocumentPointAt(changedAt))
		if err != nil || atOld == nil || atOld.ID != firstID {
			t.Fatalf("find at changed = %+v err=%v", atOld, err)
		}
		atNew, err := storage.FindDocument(ctx, address, DocumentPointAt(retiredAt))
		if err != nil || atNew == nil || atNew.ID != secondID || atNew.CreatedAt != retiredAt {
			t.Fatalf("find at retired = %+v err=%v", atNew, err)
		}
		// Scans are point-in-time and half-open.
		oldScan, err := storage.ScanDocuments(ctx, DocumentQuery{Scope: address.Scope, At: DocumentPointAt(changedAt)}, nil, 10)
		if err != nil || len(oldScan.Items) != 1 || oldScan.Items[0].ID != firstID {
			t.Fatalf("scan at changed = %+v err=%v", oldScan.Items, err)
		}
		newScan, err := storage.ScanDocuments(ctx, DocumentQuery{Scope: address.Scope, At: DocumentPointAt(retiredAt)}, nil, 10)
		if err != nil || len(newScan.Items) != 1 || newScan.Items[0].ID != secondID {
			t.Fatalf("scan at retired = %+v err=%v", newScan.Items, err)
		}
		if retired, _ := storage.Document(ctx, firstID, DocumentPointAt(retiredAt)); retired != nil {
			t.Fatalf("retired incarnation still readable: %+v", retired)
		}
		newCurrent, err := storage.Document(ctx, secondID, CurrentDocumentPoint())
		if err != nil || documentValue(t, newCurrent)["items"].([]any)[0] != "new" {
			t.Fatalf("second = %s err=%v", newCurrent.Value, err)
		}
	})
}

// TestDocumentCopyCoversUpstreamSource covers document.copy: the copy
// materializes the source's stored base at the requested point, and ambiguous
// or mismatching sources are rejected.
func TestDocumentCopyCoversUpstreamSource(t *testing.T) {
	documentBackends(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		rootID := RootConversationID
		mustCommit(t, storage, conversationWrite(rootID))
		sourceID := mustMintID(t, storage)
		history, fork := HistoryRewindable, ForkAsOf
		scope := DocumentScope{Kind: ScopeConversation, ConversationID: &rootID, History: &history, Fork: &fork}
		sourceAt := mustCommit(t, storage, StorageWrite{
			Type: "document.create", DocumentCreate: &DocumentCreate{ID: sourceID, Kind: "notes", Scope: scope},
			DocumentContent: &DocumentContent{Version: 1, Kind: ContentBase, Value: json.RawMessage(`{"v":1}`)},
		})
		mustCommit(t, storage, StorageWrite{
			Type: "document.change", DocumentID: &sourceID,
			DocumentContent: &DocumentContent{Version: 1, Kind: ContentDelta, Ops: []any{[]any{"s", []any{"v"}, 2}}},
		})

		// Copies land in their own conversations (an address holds one current
		// incarnation).
		firstChild, secondChild := mustMintID(t, storage), mustMintID(t, storage)
		mustCommit(t, storage,
			StorageWrite{Type: "conversation", Conversation: &ConversationRecord{ID: firstChild}},
			StorageWrite{Type: "conversation", Conversation: &ConversationRecord{ID: secondChild}},
		)
		childScope := func(conversationID Id) DocumentScope {
			return DocumentScope{Kind: ScopeConversation, ConversationID: &conversationID, History: &history, Fork: &fork}
		}
		historicalCopy := mustMintID(t, storage)
		currentCopy := mustMintID(t, storage)
		mustCommit(t, storage,
			StorageWrite{Type: "document.copy", DocumentCreate: &DocumentCreate{ID: historicalCopy, Kind: "notes", Scope: childScope(firstChild)},
				DocumentSource: &DocumentCopySource{ID: sourceID, At: DocumentPointAt(sourceAt)}},
			StorageWrite{Type: "document.copy", DocumentCreate: &DocumentCreate{ID: currentCopy, Kind: "notes", Scope: childScope(secondChild)},
				DocumentSource: &DocumentCopySource{ID: sourceID, At: CurrentDocumentPoint()}},
		)
		historical, err := storage.Document(ctx, historicalCopy, CurrentDocumentPoint())
		if err != nil || documentValue(t, historical)["v"].(float64) != 1 || historical.Version != 1 {
			t.Fatalf("historical copy = %s err=%v", historical.Value, err)
		}
		current, err := storage.Document(ctx, currentCopy, CurrentDocumentPoint())
		if err != nil || documentValue(t, current)["v"].(float64) != 2 {
			t.Fatalf("current copy = %s err=%v", current.Value, err)
		}

		// A source changed in the same batch is rejected (fresh addresses so
		// the rejection is about the source, not the destination).
		thirdChild := mustMintID(t, storage)
		mustCommit(t, storage, StorageWrite{Type: "conversation", Conversation: &ConversationRecord{ID: thirdChild}})
		ambiguous := mustMintID(t, storage)
		_, err = storage.Commit(ctx, []StorageWrite{
			{Type: "document.copy", DocumentCreate: &DocumentCreate{ID: ambiguous, Kind: "notes", Scope: childScope(thirdChild)},
				DocumentSource: &DocumentCopySource{ID: sourceID, At: CurrentDocumentPoint()}},
			{Type: "document.change", DocumentID: &sourceID, DocumentContent: &DocumentContent{
				Version: 1, Kind: ContentDelta, Ops: []any{[]any{"s", []any{"v"}, 3}},
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "changed in the copy batch") {
			t.Fatalf("ambiguous copy = %v", err)
		}
		// A mismatching kind is rejected.
		mismatch := mustMintID(t, storage)
		_, err = storage.Commit(ctx, []StorageWrite{
			{Type: "document.copy", DocumentCreate: &DocumentCreate{ID: mismatch, Kind: "other", Scope: childScope(thirdChild)},
				DocumentSource: &DocumentCopySource{ID: sourceID, At: CurrentDocumentPoint()}},
		})
		if err == nil || !strings.Contains(err.Error(), "does not match the copied record") {
			t.Fatalf("mismatching copy = %v", err)
		}
	})
}

// TestDocumentErrorsAndLatestHistory covers the validation paths and the
// history semantics of "latest" documents.
func TestDocumentErrorsAndLatestHistory(t *testing.T) {
	documentBackends(t, func(t *testing.T, storage Storage) {
		ctx := context.Background()
		rootID := RootConversationID
		mustCommit(t, storage, conversationWrite(rootID))
		historyLatest, forkCurrent := HistoryLatest, ForkCurrent
		scope := DocumentScope{Kind: ScopeConversation, ConversationID: &rootID, History: &historyLatest, Fork: &forkCurrent}
		id := mustMintID(t, storage)
		createdAt := mustCommit(t, storage, StorageWrite{
			Type: "document.create", DocumentCreate: &DocumentCreate{ID: id, Kind: "latest", Scope: scope},
			DocumentContent: &DocumentContent{Version: 1, Kind: ContentBase, Value: json.RawMessage(`{"n":1}`)},
		})
		changedAt := mustCommit(t, storage, StorageWrite{
			Type: "document.change", DocumentID: &id,
			DocumentContent: &DocumentContent{Version: 1, Kind: ContentDelta, Ops: []any{[]any{"s", []any{"n"}, 2}}},
		})

		// A latest document does not retain historical content.
		if _, err := storage.Document(ctx, id, DocumentPointAt(createdAt)); err == nil ||
			!strings.Contains(err.Error(), "does not retain historical content") {
			t.Fatalf("historical read = %v", err)
		}
		// A checkpoint replaces the stored history.
		mustCommit(t, storage, StorageWrite{
			Type: "document.change", DocumentID: &id,
			DocumentContent: &DocumentContent{Version: 2, Kind: ContentBase, Value: json.RawMessage(`{"n":10}`)},
		})
		current, err := storage.Document(ctx, id, CurrentDocumentPoint())
		if err != nil || documentValue(t, current)["n"].(float64) != 10 || current.DeltasSinceBase != 0 {
			t.Fatalf("current = %s err=%v", current.Value, err)
		}

		// A delta without a base is rejected on an unknown document.
		unknown := mustMintID(t, storage)
		if _, err := storage.Commit(ctx, []StorageWrite{
			{Type: "document.change", DocumentID: &unknown, DocumentContent: &DocumentContent{Version: 1, Kind: ContentDelta, Ops: []any{[]any{"s", []any{"x"}, 1}}}},
		}); err == nil || !strings.Contains(err.Error(), "Unknown document") {
			t.Fatalf("unknown change = %v", err)
		}
		// A version transition without a base is rejected.
		if _, err := storage.Commit(ctx, []StorageWrite{
			{Type: "document.change", DocumentID: &id, DocumentContent: &DocumentContent{Version: 3, Kind: ContentDelta, Ops: []any{[]any{"s", []any{"x"}, 1}}}},
		}); err == nil || !strings.Contains(err.Error(), "version transition requires a base") {
			t.Fatalf("version transition = %v", err)
		}
		// Duplicate content commands and double retirement are rejected.
		if _, err := storage.Commit(ctx, []StorageWrite{
			{Type: "document.change", DocumentID: &id, DocumentContent: &DocumentContent{Version: 2, Kind: ContentBase, Value: json.RawMessage(`{}`)}},
			{Type: "document.change", DocumentID: &id, DocumentContent: &DocumentContent{Version: 2, Kind: ContentDelta, Ops: []any{[]any{"s", []any{"x"}, 1}}}},
		}); err == nil || !strings.Contains(err.Error(), "more than one content command") {
			t.Fatalf("double content = %v", err)
		}
		if _, err := storage.Commit(ctx, []StorageWrite{
			{Type: "document.retire", DocumentID: &id},
			{Type: "document.retire", DocumentID: &id},
		}); err == nil || !strings.Contains(err.Error(), "retired more than once") {
			t.Fatalf("double retire = %v", err)
		}
		// An address may not have two current incarnations.
		second := mustMintID(t, storage)
		if _, err := storage.Commit(ctx, []StorageWrite{
			{Type: "document.create", DocumentCreate: &DocumentCreate{ID: second, Kind: "latest", Scope: scope},
				DocumentContent: &DocumentContent{Version: 1, Kind: ContentBase, Value: json.RawMessage(`{}`)}},
		}); err == nil || !strings.Contains(err.Error(), "already has a current incarnation") {
			t.Fatalf("duplicate address = %v", err)
		}
		_ = changedAt
	})
}

// TestBaseDocumentContentDeltaEncoding pins the content helpers: a delta
// content carries chord wire tuples and decodes back.
func TestBaseDocumentContentDeltaEncoding(t *testing.T) {
	content := DeltaDocumentContent(3, []delta.Op{
		{Verb: delta.VerbSet, Path: delta.Path{"a"}, Value: float64(1)},
	})
	if content.Kind != ContentDelta || content.Version != 3 || len(content.Ops) != 1 {
		t.Fatalf("content = %+v", content)
	}
	decoded, err := content.DecodedOps()
	if err != nil || len(decoded) != 1 || decoded[0].Verb != delta.VerbSet {
		t.Fatalf("decoded = %+v err=%v", decoded, err)
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"kind":"delta"`) || !strings.Contains(string(encoded), `"ops":[["s",["a"],1]]`) {
		t.Fatalf("encoded = %s", encoded)
	}
}
