package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
)

// Document state and operations of the in-memory reference storage (upstream
// storage/memory.ts): incarnation lifecycle, addressing, checkpoints and chord
// delta replay. The other backends reuse these helpers.

// storedDocumentRevision is one stored base or delta with its commit.
type storedDocumentRevision struct {
	content DocumentContent
	seq     Seq
}

// storedDocumentState is one incarnation's record and revisions.
type storedDocumentState struct {
	record    DocumentRecord
	revisions []storedDocumentRevision
}

// documentAddressIndex tracks the incarnations of one exact address.
type documentAddressIndex struct {
	ids       []Id
	currentID *Id
}

// documentAction is the merged effect of a commit's document writes.
type documentAction struct {
	create     *DocumentCreate
	content    *DocumentContent
	copySource *DocumentCopySource
	retire     bool
}

// documentScopeKey mirrors upstream scopeKey.
func documentScopeKey(scope DocumentScope) string {
	parts := []any{scope.Kind}
	switch scope.Kind {
	case ScopeConversation:
		if scope.ConversationID != nil {
			parts = append(parts, *scope.ConversationID)
		} else {
			parts = append(parts, nil)
		}
	case ScopeTask:
		if scope.TaskID != nil {
			parts = append(parts, *scope.TaskID)
		} else {
			parts = append(parts, nil)
		}
	}
	encoded, _ := json.Marshal(parts)
	return string(encoded)
}

// documentAddressKey mirrors upstream addressKey.
func documentAddressKey(address DocumentAddress) string {
	var key any = []any{"singleton"}
	if address.Key != nil {
		key = []any{"family", *address.Key}
	}
	encoded, _ := json.Marshal([]any{address.Kind, documentScopeKey(address.Scope), key})
	return string(encoded)
}

// documentRecordAddressKey is the address key of a record or create.
func documentRecordAddressKey(id Id, kind string, key *string, scope DocumentScope) string {
	return documentAddressKey(DocumentAddress{Kind: kind, Scope: scope, Key: key})
}

// isDocumentAliveAt mirrors upstream isAliveAt.
func isDocumentAliveAt(record *DocumentRecord, at DocumentPoint) bool {
	if at.Current {
		return record.RetiredAt == nil
	}
	return record.CreatedAt <= at.Seq && (record.RetiredAt == nil || at.Seq < *record.RetiredAt)
}

// isCurrentOnlyDocument mirrors upstream isCurrentOnly: only conversation
// documents with "rewindable" history retain past revisions.
func isCurrentOnlyDocument(record *DocumentRecord) bool {
	return record.Scope.Kind != ScopeConversation || record.History == nil || *record.History == HistoryLatest
}

// documentDeltaOps walks the deltas after a base, enforcing the version
// boundary (upstream documentDeltaBatches).
func documentDeltaOps(id Id, version int, revisions []storedDocumentRevision, start int) ([][]delta.Op, error) {
	batches := make([][]delta.Op, 0, len(revisions)-start)
	for index := start; index < len(revisions); index++ {
		revision := revisions[index]
		if revision.content.Kind != ContentDelta || revision.content.Version != version {
			return nil, fmt.Errorf("Document %d crosses a stored version boundary without a base", id)
		}
		ops, err := revision.content.DecodedOps()
		if err != nil {
			return nil, err
		}
		batches = append(batches, ops)
	}
	return batches, nil
}

// materializeDocument produces the stored value at a point (upstream
// materializeDocument).
func materializeDocument(state *memoryState, id Id, at DocumentPoint) (*StoredDocument, error) {
	stored, ok := state.documents[id]
	if !ok {
		return nil, nil
	}
	if !at.Current && isCurrentOnlyDocument(&stored.record) {
		return nil, fmt.Errorf("Document %d does not retain historical content", id)
	}
	if !isDocumentAliveAt(&stored.record, at) {
		return nil, nil
	}
	revisions := stored.revisions
	if !at.Current {
		filtered := make([]storedDocumentRevision, 0, len(revisions))
		for _, revision := range revisions {
			if revision.seq <= at.Seq {
				filtered = append(filtered, revision)
			}
		}
		revisions = filtered
	}
	baseIndex := len(revisions) - 1
	for baseIndex >= 0 && revisions[baseIndex].content.Kind != ContentBase {
		baseIndex--
	}
	if baseIndex < 0 {
		return nil, fmt.Errorf("Document %d is missing a required base", id)
	}
	base := revisions[baseIndex].content
	batches, err := documentDeltaOps(id, base.Version, revisions, baseIndex+1)
	if err != nil {
		return nil, err
	}
	var value chord.JsonValue = json.RawMessage(base.Value)
	if len(base.Value) == 0 {
		value = map[string]any{}
	}
	decoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var materialized any
	if err := unmarshalJSONString(string(decoded), &materialized); err != nil {
		return nil, err
	}
	for _, ops := range batches {
		materialized, err = delta.ApplyImmutable(materialized, ops)
		if err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(materialized)
	if err != nil {
		return nil, err
	}
	return &StoredDocument{
		Record:          stored.record,
		Version:         base.Version,
		Value:           encoded,
		DeltasSinceBase: len(revisions) - baseIndex - 1,
	}, nil
}

// prepareDocumentActions merges a commit's document writes (upstream
// prepareDocumentActions).
func (s *MemoryStorage) prepareDocumentActions(writes []StorageWrite) (map[Id]*documentAction, error) {
	return mergeDocumentActions(writes)
}

// mergeDocumentActions merges a batch's document writes into one action per
// document: a create or change carries at most one content command, and a
// retire is a flag applied after the content.
func mergeDocumentActions(writes []StorageWrite) (map[Id]*documentAction, error) {
	actions := map[Id]*documentAction{}
	for _, write := range writes {
		var id Id
		switch write.Type {
		case "document.create", "document.copy", "document.change", "document.retire":
			if write.Type == "document.create" || write.Type == "document.copy" {
				if write.DocumentCreate == nil {
					return nil, errors.New("document.create without a record")
				}
				id = write.DocumentCreate.ID
			} else {
				if write.DocumentID == nil {
					return nil, fmt.Errorf("%s without an id", write.Type)
				}
				id = *write.DocumentID
			}
		default:
			continue
		}
		action, ok := actions[id]
		if !ok {
			action = &documentAction{}
			actions[id] = action
		}
		switch write.Type {
		case "document.create", "document.copy":
			if action.create != nil || action.content != nil {
				return nil, fmt.Errorf("Document %d has more than one content command", id)
			}
			action.create = write.DocumentCreate
			action.content = write.DocumentContent
			if write.Type == "document.copy" {
				action.copySource = write.DocumentSource
				continue
			}
			if action.content == nil || action.content.Kind != ContentBase {
				return nil, fmt.Errorf("Document %d creation requires base content", id)
			}
		case "document.change":
			if action.content != nil {
				return nil, fmt.Errorf("Document %d has more than one content command", id)
			}
			if write.DocumentContent == nil {
				return nil, fmt.Errorf("Document %d change without content", id)
			}
			action.content = write.DocumentContent
		case "document.retire":
			if action.retire {
				return nil, fmt.Errorf("Document %d is retired more than once", id)
			}
			action.retire = true
		}
	}
	return actions, nil
}

// checkDocumentActions validates the merged actions against current state
// (upstream checkDocumentActions).
func (s *MemoryStorage) checkDocumentActions(actions map[Id]*documentAction) error {
	liveCounts := map[string]int{}
	for id, action := range actions {
		existing, exists := s.state.documents[id]
		if action.create == nil && !exists {
			return fmt.Errorf("Unknown document: %d", id)
		}
		if action.create != nil && exists {
			return fmt.Errorf("Document %d already exists", id)
		}
		if exists && existing.record.RetiredAt != nil {
			return fmt.Errorf("Document %d is retired", id)
		}
		if action.content != nil && action.content.Kind == ContentDelta {
			if !exists || len(existing.revisions) == 0 {
				return fmt.Errorf("Document %d delta has no base", id)
			}
			previous := existing.revisions[len(existing.revisions)-1]
			if previous.content.Version != action.content.Version {
				return fmt.Errorf("Document %d version transition requires a base", id)
			}
		}
		var key string
		if action.create == nil {
			key = documentRecordAddressKey(id, existing.record.Kind, existing.record.Key, existing.record.Scope)
		} else {
			key = documentRecordAddressKey(id, action.create.Kind, action.create.Key, action.create.Scope)
		}
		currentID := (*Id)(nil)
		if index, ok := s.state.documentAddresses[key]; ok {
			currentID = index.currentID
		}
		live, counted := liveCounts[key]
		if !counted {
			if currentID == nil {
				live = 0
			} else {
				live = 1
			}
		}
		if action.retire && currentID != nil && *currentID == id {
			live--
		}
		if action.create != nil && !action.retire {
			live++
		}
		liveCounts[key] = live
	}
	for _, live := range liveCounts {
		if live > 1 {
			return errors.New("Document address already has a current incarnation")
		}
	}
	return nil
}

// applyDocumentActions stamps and stores the actions (upstream
// applyDocumentActions).
func (s *MemoryStorage) applyDocumentActions(actions map[Id]*documentAction, seq Seq) {
	// Deterministic order keeps the stamping reproducible.
	ids := make([]Id, 0, len(actions))
	for id := range actions {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		action := actions[id]
		stored, exists := s.state.documents[id]
		if action.create != nil {
			record := DocumentRecord{
				ID: action.create.ID, Kind: action.create.Kind, Key: action.create.Key,
				Scope: action.create.Scope, History: action.create.History, Fork: action.create.Fork,
				CreatedAt: seq,
			}
			if action.retire {
				retired := seq
				record.RetiredAt = &retired
			}
			stored = &storedDocumentState{record: record, revisions: []storedDocumentRevision{{content: *action.content, seq: seq}}}
			s.state.documents[id] = stored
			s.state.recordTypes[id] = "document"
			key := documentRecordAddressKey(id, record.Kind, record.Key, record.Scope)
			index, ok := s.state.documentAddresses[key]
			if !ok {
				index = &documentAddressIndex{}
				s.state.documentAddresses[key] = index
			}
			index.ids = insertSorted(index.ids, id)
			scopeKey := documentScopeKey(record.Scope)
			s.state.documentIDsByScope[scopeKey] = insertSorted(s.state.documentIDsByScope[scopeKey], id)
			s.bumpNextID(id)
		} else if action.content != nil && exists {
			revision := storedDocumentRevision{content: *action.content, seq: seq}
			if action.content.Kind == ContentBase && isCurrentOnlyDocument(&stored.record) {
				stored.revisions = []storedDocumentRevision{revision}
			} else {
				stored.revisions = append(stored.revisions, revision)
			}
		}
		if action.retire && action.create == nil && exists {
			retired := seq
			stored.record.RetiredAt = &retired
		}
		if action.retire && exists && isCurrentOnlyDocument(&stored.record) {
			stored.revisions = []storedDocumentRevision{}
		}
		if (action.create != nil || action.retire) && stored != nil {
			key := documentRecordAddressKey(id, stored.record.Kind, stored.record.Key, stored.record.Scope)
			if index, ok := s.state.documentAddresses[key]; ok {
				if action.retire && index.currentID != nil && *index.currentID == id {
					index.currentID = nil
				}
				if action.create != nil && !action.retire {
					current := id
					index.currentID = &current
				}
			}
		}
	}
}

// resolveDocumentCopies turns document.copy writes into document.create base
// writes (upstream resolveDocumentCopies).
func (s *MemoryStorage) resolveDocumentCopies(writes []StorageWrite) ([]StorageWrite, error) {
	hasCopy := false
	for _, write := range writes {
		if write.Type == "document.copy" {
			hasCopy = true
			break
		}
	}
	if !hasCopy {
		return writes, nil
	}
	changed := map[Id]bool{}
	for _, write := range writes {
		switch write.Type {
		case "document.create":
			if write.DocumentCreate != nil {
				changed[write.DocumentCreate.ID] = true
			}
		case "document.copy":
			if write.DocumentCreate != nil {
				changed[write.DocumentCreate.ID] = true
			}
		case "document.change", "document.retire":
			if write.DocumentID != nil {
				changed[*write.DocumentID] = true
			}
		}
	}
	resolved := make([]StorageWrite, 0, len(writes))
	for _, write := range writes {
		if write.Type != "document.copy" {
			resolved = append(resolved, write)
			continue
		}
		if write.DocumentCreate == nil || write.DocumentSource == nil {
			return nil, errors.New("document.copy requires a record and a source")
		}
		if changed[write.DocumentSource.ID] {
			return nil, fmt.Errorf("Fork source document %d is changed in the copy batch", write.DocumentSource.ID)
		}
		stored, err := materializeDocument(&s.state, write.DocumentSource.ID, write.DocumentSource.At)
		if err != nil {
			return nil, fmt.Errorf("Document copy %d was rejected: %w", write.DocumentCreate.ID, err)
		}
		if stored == nil {
			return nil, fmt.Errorf("Fork source document %d cannot be read", write.DocumentSource.ID)
		}
		source, target := stored.Record.Scope, write.DocumentCreate.Scope
		if source.Kind != ScopeConversation || target.Kind != ScopeConversation ||
			stored.Record.Kind != write.DocumentCreate.Kind || !sameOptionalString(stored.Record.Key, write.DocumentCreate.Key) ||
			!sameOptionalString(stored.Record.History, write.DocumentCreate.History) ||
			!sameOptionalString(stored.Record.Fork, write.DocumentCreate.Fork) {
			return nil, fmt.Errorf("Fork source document %d does not match the copied record", write.DocumentSource.ID)
		}
		resolved = append(resolved, StorageWrite{
			Type:            "document.create",
			DocumentCreate:  write.DocumentCreate,
			DocumentContent: &DocumentContent{Version: stored.Version, Kind: ContentBase, Value: stored.Value},
		})
	}
	return resolved, nil
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

// FindDocument returns the incarnation alive at the point for an exact address
// (upstream findDocument).
func (s *MemoryStorage) FindDocument(ctx context.Context, address DocumentAddress, at DocumentPoint) (*DocumentRecord, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.assertOpen(); err != nil {
		return nil, err
	}
	index := s.state.documentAddresses[documentAddressKey(address)]
	if index == nil {
		return nil, nil
	}
	if at.Current {
		if index.currentID == nil {
			return nil, nil
		}
		record := s.state.documents[*index.currentID].record
		return &record, nil
	}
	for _, id := range index.ids {
		record := s.state.documents[id].record
		if isDocumentAliveAt(&record, at) {
			copied := record
			return &copied, nil
		}
	}
	return nil, nil
}

// Document materializes one incarnation at the point (upstream document).
func (s *MemoryStorage) Document(ctx context.Context, id Id, at DocumentPoint) (*StoredDocument, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.assertOpen(); err != nil {
		return nil, err
	}
	stored, err := materializeDocument(&s.state, id, at)
	if err != nil || stored == nil {
		return nil, err
	}
	return stored, nil
}

// ScanDocuments scans the incarnations alive in one scope at the point, in
// ascending id order (upstream scanDocuments).
func (s *MemoryStorage) ScanDocuments(ctx context.Context, query DocumentQuery, cursor Cursor, limit int) (Page[DocumentRecord], error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.assertOpen(); err != nil {
		return Page[DocumentRecord]{}, err
	}
	ids := s.state.documentIDsByScope[documentScopeKey(query.Scope)]
	after := cursorID(cursor)
	start := 0
	if after != nil {
		start = upperBound(ids, *after)
	}
	values := make([]DocumentRecord, 0, limit+1)
	for index := start; index < len(ids) && len(values) <= limit; index++ {
		record := s.state.documents[ids[index]].record
		if query.Kind != nil && record.Kind != *query.Kind {
			continue
		}
		if isDocumentAliveAt(&record, query.At) {
			values = append(values, record)
		}
	}
	return pageOf(values, limit, func(value DocumentRecord) Id { return value.ID }), nil
}
