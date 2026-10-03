package durable

import (
	"encoding/json"
	"sort"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
)

// Port of harness/inbox.ts: the queue of one conversation's submissions
// waiting for a boundary.

// Queue modes (upstream QueueMode).
const (
	QueueAll        = "all"
	QueueOneAtATime = "one-at-a-time"
)

// QueueModes are the settings a boundary reads.
type QueueModes struct {
	SteeringMode string
	FollowUpMode string
}

// UserInput is one submission's user content (a string or content blocks).
type UserInput = chord.JsonValue

// Inbox item modes.
const (
	InboxSteer    = "steer"
	InboxFollowUp = "followUp"
	InboxWrite    = "write"
)

// InboxItem is a queued submission: user input for a run (steer/followUp with
// content) or a passive entry write (with an EntryDraft).
type InboxItem struct {
	ID      Id              `json:"id"`
	Mode    string          `json:"mode"`
	Content chord.JsonValue `json:"content,omitempty"`
	Entry   json.RawMessage `json:"entry,omitempty"`
}

// InboxState is the built-in queue of one conversation's submissions waiting
// for a boundary, in id order.
type InboxState struct {
	Items []InboxItem `json:"items"`
}

// InboxDoc is the conversation-scoped inbox document (latest history, initial
// fork); it checkpoints whenever the queue is empty.
var InboxDoc = mustDefineDoc(DocDefinition{
	Kind: "pi.inbox", Version: 1, Scope: ScopeConversation,
	History: stringPointer(HistoryLatest), Fork: stringPointer(ForkInitial),
	Initial: func(seed chord.JsonValue) (chord.JsonValue, error) {
		return map[string]any{"items": []any{}}, nil
	},
	CheckpointWhen: func(value chord.JsonValue, ops []delta.Op, info CheckpointInfo) bool {
		items, _ := value.(map[string]any)["items"].([]any)
		return len(items) == 0
	},
})

// Boundary is what a boundary reads before the commit's first table write.
type Boundary struct {
	ConversationID Id
	// Inbox is the live inbox draft.
	Inbox chord.JsonValue
	// SteeringMode and FollowUpMode come from the settings.
	SteeringMode string
	FollowUpMode string
	// Head is the start of the active range; heads written in this commit
	// advance it.
	Head *Id
}

// BoundaryResult is the selected user items in id order and whether a
// `head: "self"` write (a reset) was placed.
type BoundaryResult struct {
	Users []Id
	Reset bool
}

// PrepareBoundary reads what a boundary needs; table reads must precede the
// commit's first table write.
func PrepareBoundary(tx *Transaction, conversationID Id, modes QueueModes) (*Boundary, error) {
	marker, err := tx.LatestHeadMarker(conversationID)
	if err != nil {
		return nil, err
	}
	var head *Id
	if marker != nil {
		head = marker.Head
	}
	inbox, err := tx.Doc(InboxDoc.Definition, conversationID)
	if err != nil {
		return nil, err
	}
	return &Boundary{
		ConversationID: conversationID, Inbox: inbox,
		SteeringMode: modes.SteeringMode, FollowUpMode: modes.FollowUpMode, Head: head,
	}, nil
}

// ApplyBoundary places the queued items a boundary selects: every write, the
// first or all steers, and at final the first or all follow-ups. A selected
// reset turns a postTools boundary into final.
func ApplyBoundary(tx *Transaction, boundary *Boundary, at string, now int64) (BoundaryResult, error) {
	state, err := decodeInboxState(boundary.Inbox)
	if err != nil {
		return BoundaryResult{}, err
	}
	items := state.Items
	reset := false
	for _, item := range items {
		if item.Mode != InboxWrite {
			continue
		}
		draft, err := decodeInboxEntry(item)
		if err == nil && draft.HeadSelf {
			reset = true
			break
		}
	}
	final := at == "final" || reset
	pick := func(mode string, queueMode string) []int {
		indexes := []int{}
		for index, item := range items {
			if item.Mode == mode {
				indexes = append(indexes, index)
			}
		}
		if queueMode == QueueAll {
			return indexes
		}
		if len(indexes) > 1 {
			return indexes[:1]
		}
		return indexes
	}
	var writes []int
	for index, item := range items {
		if item.Mode == InboxWrite {
			writes = append(writes, index)
		}
	}
	users := pick(InboxSteer, boundary.SteeringMode)
	if final {
		users = append(users, pick(InboxFollowUp, boundary.FollowUpMode)...)
	}
	sort.Ints(users)

	// The drafts' values are copied into the appended entries; the items are
	// removed only afterwards.
	for _, index := range writes {
		item := items[index]
		draft, err := decodeInboxEntry(item)
		if err != nil {
			return BoundaryResult{}, err
		}
		if IsStale(boundary, draft) {
			reason := "stale"
			if err := tx.SettleSubmission(item.ID, SubmissionSettlement{Status: SubmissionUnanswered, Reason: &reason}); err != nil {
				return BoundaryResult{}, err
			}
			continue
		}
		entry, err := tx.AppendEntry(boundary.ConversationID, draft)
		if err != nil {
			return BoundaryResult{}, err
		}
		switch {
		case draft.HeadSelf:
			head := entry.ID
			boundary.Head = &head
		case draft.Head != nil:
			head := *draft.Head
			boundary.Head = &head
		}
		if err := tx.PlaceSubmission(item.ID, entry.ID); err != nil {
			return BoundaryResult{}, err
		}
	}
	placed := []Id{}
	for _, index := range users {
		item := items[index]
		message, err := userMessageFromContent(item.Content, now)
		if err != nil {
			return BoundaryResult{}, err
		}
		entry, err := tx.AppendEntry(boundary.ConversationID, EntryDraft{Kind: UserEntry.Kind, Model: []ai.Message{message}})
		if err != nil {
			return BoundaryResult{}, err
		}
		if err := tx.PlaceSubmission(item.ID, entry.ID); err != nil {
			return BoundaryResult{}, err
		}
		placed = append(placed, item.ID)
	}
	removed := append(append([]int{}, writes...), users...)
	sort.Sort(sort.Reverse(sort.IntSlice(removed)))
	for _, index := range removed {
		items = append(items[:index], items[index+1:]...)
	}
	if container, ok := boundary.Inbox.(map[string]any); ok {
		AssignJSON(container, "items", inboxItemsValue(items))
	}
	return BoundaryResult{Users: placed, Reset: reset}, nil
}

// IsStale reports whether a head write targets an entry before the active
// range.
func IsStale(boundary *Boundary, entry EntryDraft) bool {
	return entry.Head != nil && boundary.Head != nil && *entry.Head < *boundary.Head
}

// RemoveInboxItem removes a withdrawn submission's item; the caller settles
// the submission.
func RemoveInboxItem(tx *Transaction, conversationID Id, id Id) error {
	inbox, err := tx.Doc(InboxDoc.Definition, conversationID)
	if err != nil {
		return err
	}
	state, err := decodeInboxState(inbox)
	if err != nil {
		return err
	}
	items := state.Items
	for index, item := range items {
		if item.ID == id {
			items = append(items[:index], items[index+1:]...)
			break
		}
	}
	if container, ok := inbox.(map[string]any); ok {
		AssignJSON(container, "items", inboxItemsValue(items))
	}
	return nil
}

// WithdrawQueuedInputs withdraws every queued input of a conversation: each
// settles unanswered with reason aborted; queued writes stay.
func WithdrawQueuedInputs(tx *Transaction, conversationID Id) error {
	inbox, err := tx.Doc(InboxDoc.Definition, conversationID)
	if err != nil {
		return err
	}
	state, err := decodeInboxState(inbox)
	if err != nil {
		return err
	}
	items := state.Items
	for index := len(items) - 1; index >= 0; index-- {
		item := items[index]
		if item.Mode == InboxWrite {
			continue
		}
		reason := "aborted"
		if err := tx.SettleSubmission(item.ID, SubmissionSettlement{Status: SubmissionUnanswered, Reason: &reason}); err != nil {
			return err
		}
		items = append(items[:index], items[index+1:]...)
	}
	if container, ok := inbox.(map[string]any); ok {
		AssignJSON(container, "items", inboxItemsValue(items))
	}
	return nil
}

func decodeInboxState(inbox chord.JsonValue) (InboxState, error) {
	state := InboxState{Items: []InboxItem{}}
	encoded, err := marshalJSONValue(inbox)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal([]byte(encoded), &state); err != nil {
		return state, err
	}
	if state.Items == nil {
		state.Items = []InboxItem{}
	}
	return state, nil
}

func decodeInboxEntry(item InboxItem) (EntryDraft, error) {
	var draft EntryDraft
	if len(item.Entry) == 0 {
		return draft, nil
	}
	err := json.Unmarshal(item.Entry, &draft)
	return draft, err
}

// userMessageFromContent builds a user message from stored content.
func userMessageFromContent(content chord.JsonValue, now int64) (*ai.UserMessage, error) {
	encoded, err := marshalJSONValue(content)
	if err != nil {
		return nil, err
	}
	var blocks ai.StringOrBlocks
	if err := json.Unmarshal([]byte(encoded), &blocks); err != nil {
		return nil, err
	}
	return &ai.UserMessage{Content: blocks, Timestamp: now}, nil
}

func inboxItemsValue(items []InboxItem) chord.JsonValue {
	if items == nil {
		items = []InboxItem{}
	}
	encoded, err := marshalJSONValue(items)
	if err != nil {
		return []any{}
	}
	var value chord.JsonValue
	if err := json.Unmarshal([]byte(encoded), &value); err != nil {
		return []any{}
	}
	return value
}
