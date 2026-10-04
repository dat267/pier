package durable

// Port of harness/provider.ts: the stable provider-facing identity of one
// conversation, forwarded as the model request's session id so a cache-aware
// provider can reuse it.

import (
	"fmt"

	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
	"github.com/dat267/pier/coding"
)

// ProviderState is the provider-facing identity of one conversation.
type ProviderState struct {
	SessionID string `json:"sessionId"`
}

// ProviderDoc is the conversation-scoped provider identity document (latest
// history; every fork starts with a fresh identity instead of copying its
// parent).
var ProviderDoc = mustDefineDoc(DocDefinition{
	Kind: "pi.provider", Version: 1, Scope: ScopeConversation,
	History: stringPointer(HistoryLatest), Fork: stringPointer(ForkInitial),
	Initial: func(seed chord.JsonValue) (chord.JsonValue, error) {
		return map[string]any{"sessionId": coding.UUIDv7()}, nil
	},
	CheckpointWhen: func(value chord.JsonValue, ops []delta.Op, info CheckpointInfo) bool { return true },
})

// EnsureProviderSessionID returns the persisted identity without writing in the
// normal path. A legacy conversation without pi.provider gets one migration
// commit whose tx.Doc runs Initial before the provider request starts
// (upstream ensureProviderSessionId).
func EnsureProviderSessionID(runtime TaskRuntime, context chord.Context) (string, error) {
	existing, present, err := runtime.Snapshot(context, ProviderDoc.Definition, runtime.ConversationID())
	if err != nil {
		return "", err
	}
	if present {
		if state, ok := decodeJSONInto[ProviderState](existing); ok && state.SessionID != "" {
			return state.SessionID, nil
		}
	}
	var created string
	if err := runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		value, err := tx.Doc(ProviderDoc.Definition, runtime.ConversationID())
		if err != nil {
			return nil, err
		}
		if state, ok := decodeJSONInto[ProviderState](value); ok {
			created = state.SessionID
		}
		return nil, nil
	}, context); err != nil {
		return "", err
	}
	if created == "" {
		return "", fmt.Errorf("Conversation %d has no provider session ID", runtime.ConversationID())
	}
	return created, nil
}
