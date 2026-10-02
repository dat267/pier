package durable

import (
	"context"
	"fmt"
)

// Port of session/forks.ts: the definition-free document copies one fork
// creates.

// ForkDocumentCopy is one definition-free document copy to create with a
// forked conversation (upstream ForkDocumentCopy).
type ForkDocumentCopy struct {
	Record *DocumentCreate
	Source DocumentCopySource
}

// scanPageSize is upstream SCAN_PAGE_SIZE.
const scanPageSize = 256

// PrepareForkDocumentCopies selects every persisted conversation document
// copied by one fork (upstream prepareForkDocumentCopies): the source
// conversation's documents as of the fork entry, plus the parent
// conversation's current-only documents.
func PrepareForkDocumentCopies(
	ctx context.Context,
	storage Storage,
	parentConversationID Id,
	at Id,
	childConversationID Id,
) ([]ForkDocumentCopy, error) {
	commit, err := visibleEntry(ctx, storage, parentConversationID, at)
	if err != nil {
		return nil, err
	}
	if commit == nil {
		return nil, fmt.Errorf("Entry %d is not visible from conversation %d", at, parentConversationID)
	}
	var copies []ForkDocumentCopy
	copied := map[string]struct{}{}
	sourceScope := DocumentScope{Kind: ScopeConversation, ConversationID: &commit.Entry.ConversationID}
	if err := collectForkCopies(ctx, storage, sourceScope, DocumentPointAt(commit.CommitSeq), ForkAsOf,
		childConversationID, &copies, copied); err != nil {
		return nil, err
	}
	parentScope := DocumentScope{Kind: ScopeConversation, ConversationID: &parentConversationID}
	if err := collectForkCopies(ctx, storage, parentScope, CurrentDocumentPoint(), ForkCurrent,
		childConversationID, &copies, copied); err != nil {
		return nil, err
	}
	return copies, nil
}

// visibleEntry looks up one entry that is visible from a conversation
// (upstream's conversation-scoped Storage.entry).
func visibleEntry(ctx context.Context, storage Storage, conversationID Id, at Id) (*EntryCommit, error) {
	page, err := storage.ScanEntries(ctx, EntryQuery{ConversationID: conversationID, MinEntryID: &at, MaxEntryID: &at}, nil, 1)
	if err != nil {
		return nil, err
	}
	if len(page.Items) == 0 || page.Items[0].ID != at {
		return nil, nil
	}
	return storage.Entry(ctx, at)
}

// collectForkCopies appends the copies selected from one scope and policy
// (upstream collectCopies).
func collectForkCopies(
	ctx context.Context,
	storage Storage,
	scope DocumentScope,
	at DocumentPoint,
	policy string,
	childConversationID Id,
	copies *[]ForkDocumentCopy,
	copied map[string]struct{},
) error {
	var cursor Cursor
	for {
		page, err := storage.ScanDocuments(ctx, DocumentQuery{Scope: scope, At: at}, cursor, scanPageSize)
		if err != nil {
			return err
		}
		for _, source := range page.Items {
			if source.Scope.Kind != ScopeConversation || source.Fork == nil || *source.Fork != policy {
				continue
			}
			id, err := storage.MintID(ctx)
			if err != nil {
				return err
			}
			record := &DocumentCreate{
				ID:    id,
				Kind:  source.Kind,
				Key:   source.Key,
				Scope: DocumentScope{Kind: ScopeConversation, ConversationID: &childConversationID},
				Fork:  source.Fork,
			}
			if source.History != nil && *source.History == HistoryLatest {
				history := HistoryLatest
				record.History = &history
			} else {
				history := HistoryRewindable
				record.History = &history
			}
			address := AddressID(DocumentAddress{Kind: record.Kind, Scope: record.Scope, Key: record.Key})
			if _, duplicate := copied[address]; duplicate {
				member := record.Kind
				if record.Key != nil {
					member = record.Kind + "/" + *record.Key
				}
				return fmt.Errorf("Fork selects multiple source documents for %s", member)
			}
			copied[address] = struct{}{}
			*copies = append(*copies, ForkDocumentCopy{
				Record: record,
				Source: DocumentCopySource{ID: source.ID, At: at},
			})
		}
		cursor = page.Next
		if cursor == nil {
			return nil
		}
	}
}
