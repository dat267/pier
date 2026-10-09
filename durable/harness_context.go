package durable

import (
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of harness/context.ts: the committed transcript context of one
// conversation.

// contextScanPageSize is upstream SCAN_PAGE_SIZE.
const contextScanPageSize = 256

// missingResultText is upstream MISSING_RESULT_TEXT.
const missingResultText = "Tool result unavailable: history ends before this call completed."

// excludedStopReasons are assistant stop reasons whose message leaves the model
// context (upstream EXCLUDED_STOP_REASONS).
var excludedStopReasons = map[ai.StopReason]struct{}{
	ai.StopAborted:  {},
	ai.StopError:    {},
	ai.StopDeferred: {},
}

// ContextBounds is the head marker and newest visible entry that fix one
// committed context range.
type ContextBounds struct {
	// Head is the newest head marker, if any.
	Head *EntryRecord
	// Tail is the newest visible entry.
	Tail Id
}

// contextRange holds one conversation's reusable scanned history and derived context.
type contextRange struct {
	conversationID Id
	bounds         *ContextBounds
	entries        []EntryRecord
	view           ContextView
	edited         map[Id]struct{}
	settled        []ai.Message
	open           []ai.Message
}

// ContextView is the raw active transcript and the derived model context.
type ContextView struct {
	// Head is the newest applicable head marker, if any.
	Head *EntryRecord
	// Entries are the active entries: the head marker followed by non-head
	// entries from its head through the tail.
	Entries []EntryRecord
	// Contributions are each entry's model messages after edits and excluded
	// stop reasons, before tool-result ordering.
	Contributions [][]ai.Message
	// Messages are the model context for the next provider request.
	Messages []ai.Message
}

// CaptureContextBounds captures the bounds with two O(1) reads; run it on the
// session line.
func CaptureContextBounds(ctx chord.Context, storage Storage, conversationID Id, at *Id) (*ContextBounds, error) {
	var tail Id
	if at == nil {
		page, err := storage.ScanEntries(ctx, EntryQuery{ConversationID: conversationID}, nil, 1)
		if err != nil {
			return nil, err
		}
		if len(page.Items) == 0 {
			return nil, nil
		}
		tail = page.Items[0].ID
	} else {
		commit, err := visibleEntry(ctx, storage, conversationID, *at)
		if err != nil {
			return nil, err
		}
		if commit == nil {
			return nil, &harnessContextError{message: "Entry " + itoaID(*at) + " is not visible from conversation " + itoaID(conversationID)}
		}
		tail = *at
	}
	head, err := storage.FindLatestHeadMarker(ctx, conversationID, &tail)
	if err != nil {
		return nil, err
	}
	return &ContextBounds{Head: head, Tail: tail}, nil
}

// ReadContext captures the bounds on the session line and derives the context
// off it.
func ReadContext(ctx chord.Context, session *Session, storage Storage, conversationID Id, at *Id) (ContextView, error) {
	view, _, err := readContextFrom(ctx, session, storage, conversationID, at, nil)
	return view, err
}

// readContextFrom extends a prior range when conversation and head marker still match.
func readContextFrom(ctx chord.Context, session *Session, storage Storage, conversationID Id, at *Id, previous *contextRange) (ContextView, *contextRange, error) {
	var bounds *ContextBounds
	var captureErr error
	if err := session.ReadOnLine(func() error {
		bounds, captureErr = CaptureContextBounds(ctx, storage, conversationID, at)
		return captureErr
	}); err != nil {
		return ContextView{}, nil, err
	}
	if bounds == nil {
		return ContextView{Entries: []EntryRecord{}, Contributions: [][]ai.Message{}, Messages: []ai.Message{}}, nil, nil
	}

	var current *contextRange
	switch {
	case previous == nil || previous.conversationID != conversationID || !sameContextHead(previous.bounds, bounds):
		entries, err := scanContextRange(ctx, storage, conversationID, bounds)
		if err != nil {
			return ContextView{}, nil, err
		}
		current = deriveContextRange(conversationID, bounds, entries)
	case bounds.Tail == previous.bounds.Tail:
		current = previous
	case bounds.Tail < previous.bounds.Tail:
		entries := make([]EntryRecord, 0, len(previous.entries))
		for _, entry := range previous.entries {
			if entry.ID <= bounds.Tail {
				entries = append(entries, entry)
			}
		}
		current = deriveContextRange(conversationID, bounds, entries)
	default:
		minEntryID := previous.bounds.Tail + 1
		added, err := scanContextEntries(ctx, storage, EntryQuery{
			ConversationID: conversationID, MinEntryID: &minEntryID, MaxEntryID: &bounds.Tail,
		})
		if err != nil {
			return ContextView{}, nil, err
		}
		if contextRangeNeedsDerivation(previous, added) {
			entries := make([]EntryRecord, 0, len(previous.entries)+len(added))
			entries = append(entries, previous.entries...)
			entries = append(entries, added...)
			current = deriveContextRange(conversationID, bounds, entries)
		} else {
			current = extendContextRange(previous, bounds, added)
		}
	}
	view, err := cloneContextView(current.view)
	if err != nil {
		return ContextView{}, nil, err
	}
	return view, current, nil
}

func sameContextHead(left, right *ContextBounds) bool {
	if left.Head == nil || right.Head == nil {
		return left.Head == nil && right.Head == nil
	}
	return left.Head.ID == right.Head.ID
}

// DeriveContext derives the active transcript and model context within
// captured bounds.
func DeriveContext(ctx chord.Context, storage Storage, conversationID Id, bounds *ContextBounds) (ContextView, error) {
	if bounds == nil {
		return ContextView{Entries: []EntryRecord{}, Contributions: [][]ai.Message{}, Messages: []ai.Message{}}, nil
	}
	rangeEntries, err := scanContextRange(ctx, storage, conversationID, bounds)
	if err != nil {
		return ContextView{}, err
	}
	return deriveContextRange(conversationID, bounds, rangeEntries).view, nil
}

func deriveContextRange(conversationID Id, bounds *ContextBounds, rangeEntries []EntryRecord) *contextRange {
	edits := map[Id]ContextEdit{}
	edited := map[Id]struct{}{}
	// Edits of every entry in the range count, including older head markers.
	for _, entry := range rangeEntries {
		for _, edit := range entry.Edits {
			edits[edit.Target] = edit
			edited[edit.Target] = struct{}{}
		}
	}
	entries := selectActive(bounds.Head, rangeEntries)
	contributions := make([][]ai.Message, 0, len(entries))
	for _, entry := range entries {
		contributions = append(contributions, contributeContext(entry, edits[entry.ID]))
	}
	settled, open := settleContextMessages(nil, flattenMessages(contributions))
	messages := make([]ai.Message, 0, len(settled)+len(open))
	messages = append(messages, settled...)
	messages = append(messages, OrderToolResults(open)...)
	view := ContextView{
		Head: bounds.Head, Entries: entries, Contributions: contributions,
		Messages: leadWithSystem(messages),
	}
	return &contextRange{
		conversationID: conversationID, bounds: bounds, entries: rangeEntries, view: view,
		edited: edited, settled: settled, open: open,
	}
}

func contextRangeNeedsDerivation(previous *contextRange, added []EntryRecord) bool {
	for _, entry := range added {
		if entry.Head != nil || len(entry.Edits) > 0 {
			return true
		}
		if _, edited := previous.edited[entry.ID]; edited {
			return true
		}
	}
	return false
}

func extendContextRange(previous *contextRange, bounds *ContextBounds, added []EntryRecord) *contextRange {
	entries := make([]EntryRecord, 0, len(previous.entries)+len(added))
	entries = append(entries, previous.entries...)
	entries = append(entries, added...)
	newContributions := make([][]ai.Message, 0, len(added))
	newMessages := make([]ai.Message, 0)
	for _, entry := range added {
		contribution := contributeContext(entry, ContextEdit{})
		newContributions = append(newContributions, contribution)
		newMessages = append(newMessages, contribution...)
	}
	open := make([]ai.Message, 0, len(previous.open)+len(newMessages))
	open = append(open, previous.open...)
	open = append(open, newMessages...)
	settled, open := settleContextMessages(previous.settled, open)
	active := make([]EntryRecord, 0, len(previous.view.Entries)+len(added))
	active = append(active, previous.view.Entries...)
	active = append(active, added...)
	contributions := make([][]ai.Message, 0, len(previous.view.Contributions)+len(newContributions))
	contributions = append(contributions, previous.view.Contributions...)
	contributions = append(contributions, newContributions...)
	messages := make([]ai.Message, 0, len(settled)+len(open))
	messages = append(messages, settled...)
	messages = append(messages, OrderToolResults(open)...)
	view := ContextView{
		Head: bounds.Head, Entries: active, Contributions: contributions,
		Messages: leadWithSystem(messages),
	}
	return &contextRange{
		conversationID: previous.conversationID, bounds: bounds, entries: entries, view: view,
		edited: previous.edited, settled: settled, open: open,
	}
}

func contributeContext(entry EntryRecord, edit ContextEdit) []ai.Message {
	if edit.Action == EditOmit {
		return []ai.Message{}
	}
	contributed := entry.Model
	if edit.Action == EditReplace {
		contributed = edit.Messages
	}
	filtered := make([]ai.Message, 0, len(contributed))
	for _, message := range contributed {
		if assistant, ok := message.(*ai.AssistantMessage); ok {
			if _, excluded := excludedStopReasons[assistant.StopReason]; excluded {
				continue
			}
		}
		filtered = append(filtered, message)
	}
	return filtered
}

func settleContextMessages(settled, open []ai.Message) ([]ai.Message, []ai.Message) {
	lastAssistant := -1
	for index, message := range open {
		if _, ok := message.(*ai.AssistantMessage); ok {
			lastAssistant = index
		}
	}
	if lastAssistant <= 0 {
		return settled, open
	}
	ready := OrderToolResults(open[:lastAssistant])
	combined := make([]ai.Message, 0, len(settled)+len(ready))
	combined = append(combined, settled...)
	combined = append(combined, ready...)
	return combined, append([]ai.Message(nil), open[lastAssistant:]...)
}

func cloneContextView(view ContextView) (ContextView, error) {
	copyMessage := func(message ai.Message) (ai.Message, error) {
		encoded, err := ai.MarshalMessage(message)
		if err != nil {
			return nil, err
		}
		return ai.UnmarshalMessage(encoded)
	}
	cloneMessages := func(messages []ai.Message) ([]ai.Message, error) {
		result := make([]ai.Message, 0, len(messages))
		for _, message := range messages {
			clone, err := copyMessage(message)
			if err != nil {
				return nil, err
			}
			result = append(result, clone)
		}
		return result, nil
	}
	result := ContextView{
		Entries:       make([]EntryRecord, 0, len(view.Entries)),
		Contributions: make([][]ai.Message, 0, len(view.Contributions)),
		Messages:      make([]ai.Message, 0, len(view.Messages)),
	}
	if view.Head != nil {
		result.Head = cloneEntry(view.Head)
	}
	for index := range view.Entries {
		entry := cloneEntry(&view.Entries[index])
		result.Entries = append(result.Entries, *entry)
	}
	for _, messages := range view.Contributions {
		cloned, err := cloneMessages(messages)
		if err != nil {
			return ContextView{}, err
		}
		result.Contributions = append(result.Contributions, cloned)
	}
	messages, err := cloneMessages(view.Messages)
	if err != nil {
		return ContextView{}, err
	}
	result.Messages = append(result.Messages, messages...)
	return result, nil
}

// ActiveEntries returns the raw active entries within captured bounds.
func ActiveEntries(ctx chord.Context, storage Storage, conversationID Id, bounds *ContextBounds) ([]EntryRecord, error) {
	if bounds == nil {
		return []EntryRecord{}, nil
	}
	rangeEntries, err := scanContextRange(ctx, storage, conversationID, bounds)
	if err != nil {
		return nil, err
	}
	return selectActive(bounds.Head, rangeEntries), nil
}

// scanContextRange reads the visible entries from the head marker's head, or
// the transcript start, through the tail, oldest first.
func scanContextRange(ctx chord.Context, storage Storage, conversationID Id, bounds *ContextBounds) ([]EntryRecord, error) {
	query := EntryQuery{ConversationID: conversationID, MaxEntryID: &bounds.Tail}
	if bounds.Head != nil && bounds.Head.Head != nil {
		query.MinEntryID = bounds.Head.Head
	}
	return scanContextEntries(ctx, storage, query)
}

func scanContextEntries(ctx chord.Context, storage Storage, query EntryQuery) ([]EntryRecord, error) {
	var entries []EntryRecord
	var cursor Cursor
	for {
		page, err := storage.ScanEntries(ctx, query, cursor, contextScanPageSize)
		if err != nil {
			return nil, err
		}
		entries = append(entries, page.Items...)
		cursor = page.Next
		if cursor == nil {
			break
		}
	}
	// Newest-first becomes oldest-first.
	for left, right := 0, len(entries)-1; left < right; left, right = left+1, right-1 {
		entries[left], entries[right] = entries[right], entries[left]
	}
	return entries, nil
}

// selectActive is the head marker followed by the range's non-head entries, or
// the whole range without a marker.
func selectActive(head *EntryRecord, rangeEntries []EntryRecord) []EntryRecord {
	if head == nil {
		return rangeEntries
	}
	active := make([]EntryRecord, 0, len(rangeEntries)+1)
	active = append(active, *head)
	for _, entry := range rangeEntries {
		if entry.Head == nil {
			active = append(active, entry)
		}
	}
	return active
}

// OrderToolResults places each assistant's tool results directly after it in
// call order. A missing result is synthesized and unmatched results are
// dropped.
func OrderToolResults(messages []ai.Message) []ai.Message {
	ordered := make([]ai.Message, 0, len(messages))
	for index := 0; index < len(messages); index++ {
		message := messages[index]
		if _, isResult := message.(*ai.ToolResultMessage); isResult {
			continue
		}
		ordered = append(ordered, message)
		assistant, isAssistant := message.(*ai.AssistantMessage)
		if !isAssistant {
			continue
		}
		var calls []ai.ToolCall
		for _, content := range assistant.Content {
			if call, ok := content.(ai.ToolCall); ok {
				calls = append(calls, call)
			}
		}
		if len(calls) == 0 {
			continue
		}
		results := map[string]int{}
		for next := index + 1; next < len(messages); next++ {
			if _, isAssistant := messages[next].(*ai.AssistantMessage); isAssistant {
				break
			}
			if result, ok := messages[next].(*ai.ToolResultMessage); ok {
				if _, seen := results[result.ToolCallID]; !seen {
					results[result.ToolCallID] = next
				}
			}
		}
		for _, call := range calls {
			if resultIndex, present := results[call.ID]; present {
				ordered = append(ordered, messages[resultIndex])
			} else {
				ordered = append(ordered, MissingToolResult(call, assistant.Timestamp))
			}
		}
	}
	return ordered
}

// MissingToolResult synthesizes the result of a call whose completion is
// missing from the visible history.
func MissingToolResult(call ai.ToolCall, timestamp int64) *ai.ToolResultMessage {
	return &ai.ToolResultMessage{
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Content:    ai.UserContentList{ai.TextContent{Text: missingResultText}},
		IsError:    true,
		Details:    []byte(`{"reason":"missing_result"}`),
		Timestamp:  timestamp,
	}
}

// D212: upstream v1.0.2 leaves a baseline system message after the initial
// user input because generation commits input before preparing the prompt.
// Follow v1.1.0: providers treat only a leading system message as the initial
// prompt and tool set, so move the first non-user message to the front only
// when it is a system message. Later system messages retain their positions.
func leadWithSystem(messages []ai.Message) []ai.Message {
	index := 0
	for index < len(messages) {
		if _, isUser := messages[index].(*ai.UserMessage); !isUser {
			break
		}
		index++
	}
	if index == 0 || index == len(messages) {
		return messages
	}
	if _, isSystem := messages[index].(*ai.SystemMessage); !isSystem {
		return messages
	}
	ordered := make([]ai.Message, 0, len(messages))
	ordered = append(ordered, messages[index])
	ordered = append(ordered, messages[:index]...)
	ordered = append(ordered, messages[index+1:]...)
	return ordered
}

func flattenMessages(contributions [][]ai.Message) []ai.Message {
	total := 0
	for _, group := range contributions {
		total += len(group)
	}
	messages := make([]ai.Message, 0, total)
	for _, group := range contributions {
		messages = append(messages, group...)
	}
	return messages
}

// harnessContextError is an ordinary context derivation failure.
type harnessContextError struct{ message string }

func (e *harnessContextError) Error() string { return e.message }

// itoaID renders an id for a message.
func itoaID(id Id) string {
	if id == 0 {
		return "0"
	}
	negative := id < 0
	value := id
	if negative {
		value = -value
	}
	var digits [20]byte
	position := len(digits)
	for value > 0 {
		position--
		digits[position] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		position--
		digits[position] = '-'
	}
	return string(digits[position:])
}
