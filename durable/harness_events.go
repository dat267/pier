package durable

import (
	"encoding/json"
	"reflect"
	"sort"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
)

// Port of harness/events.ts: the experimental agent event stream of one
// conversation (spec §9.4).

// MessageChange is one change to the in-flight assistant message, relative to
// that message.
type MessageChange struct {
	// Type is text_start/thinking_start/toolcall_start/text_delta/
	// thinking_delta/toolcall_delta/block/message.
	Type         string
	ContentIndex int
	Block        ai.Content
	Delta        string
	// Path is the relative arguments path of a toolcall_delta.
	Path []any
	// Message is set for a whole-message change.
	Message *ai.AssistantMessage
}

// Message change types.
const (
	MessageTextStart     = "text_start"
	MessageThinkingStart = "thinking_start"
	MessageToolcallStart = "toolcall_start"
	MessageTextDelta     = "text_delta"
	MessageThinkingDelta = "thinking_delta"
	MessageToolcallDelta = "toolcall_delta"
	MessageBlock         = "block"
	MessageWhole         = "message"
)

// QueuedItem is one queued inbox item's identity and mode.
type QueuedItem struct {
	ID   Id     `json:"id"`
	Mode string `json:"mode"`
}

// RunInputs is the run control of a snapshot or run event.
type RunInputs struct {
	Inputs []Id `json:"inputs"`
}

// SnapshotGeneration is the current generation attempt in a snapshot.
type SnapshotGeneration struct {
	Attempt  int                  `json:"attempt"`
	Message  *ai.AssistantMessage `json:"message,omitempty"`
	Retry    *LiveRetry           `json:"retry,omitempty"`
	Deferred *LiveDeferred        `json:"deferred,omitempty"`
}

// ToolOutput is a front trim and an append of the retained window, or its
// replacement.
type ToolOutput struct {
	// Set replaces the whole output when non-nil.
	Set       *string
	TrimStart *int
	Append    *string
}

// AgentEvent is one event of the stream; Type discriminates the populated
// fields.
type AgentEvent struct {
	Type string

	// snapshot
	Entries     []EntryRecord
	Run         *RunInputs
	Generation  *SnapshotGeneration
	Tools       []ToolSlot
	Compactions []CompactionStatus
	Inbox       []QueuedItem
	Agent       *AgentState
	Usage       *UsageState

	// run_start / run_end
	Inputs []Id

	// message_start / message_update
	Message     ai.Message
	UsageUpdate *ai.Usage
	Changes     []MessageChange

	// message_end / entry_appended
	Entry *EntryRecord

	// tool_execution_start / tool_execution_update / tool_execution_end
	ToolCallID  string
	ToolName    string
	Args        JsonObject
	Output      *ToolOutput
	Details     chord.JsonValue
	Diagnostics []ToolDiagnostic

	// submission
	Record *SubmissionRecord

	// auto_retry_start / auto_retry_end / deferred_poll
	Attempt      int
	At           int64
	ErrorMessage string
	PollAt       int64

	// task_failed
	TaskID Id
	Kind   string

	// compaction_start / compaction_end
	Reason   CompactionReason
	Blocking bool
}

// Agent event types.
const (
	EventSnapshot            = "snapshot"
	EventRunStart            = "run_start"
	EventRunEnd              = "run_end"
	EventTurnStart           = "turn_start"
	EventTurnEnd             = "turn_end"
	EventMessageStart        = "message_start"
	EventMessageUpdate       = "message_update"
	EventMessageEnd          = "message_end"
	EventToolExecutionStart  = "tool_execution_start"
	EventToolExecutionUpdate = "tool_execution_update"
	EventToolExecutionEnd    = "tool_execution_end"
	EventInboxUpdate         = "inbox_update"
	EventSubmission          = "submission"
	EventAutoRetryStart      = "auto_retry_start"
	EventAutoRetryEnd        = "auto_retry_end"
	EventDeferredPoll        = "deferred_poll"
	EventEntryAppended       = "entry_appended"
	EventAgentChanged        = "agent_changed"
	EventUsageChanged        = "usage_changed"
	EventTaskFailed          = "task_failed"
	EventCompactionStart     = "compaction_start"
	EventCompactionEnd       = "compaction_end"
)

// AgentEventListener receives one batch per commit.
type AgentEventListener func(events []AgentEvent, ctx chord.Context) error

// AgentEventStream is the serialized stream of one conversation's event
// batches, one per commit.
type AgentEventStream struct {
	Snapshot AgentEvent
	Start    func(listener AgentEventListener)
	Stop     func() WatchEnd
	Closed   func() <-chan struct{}
	End      func() (WatchEnd, bool)
}

type eventPartSet struct {
	live  LiveState
	inbox *InboxState
	agent *AgentState
	usage *UsageState
	// raws keep the document values for the identity comparisons the event
	// rules use.
	liveRaw  chord.JsonValue
	inboxRaw chord.JsonValue
	agentRaw chord.JsonValue
	usageRaw chord.JsonValue
}

func eventParts(view ConversationView) eventPartSet {
	parts := eventPartSet{
		liveRaw: view.Docs["pi.live"], inboxRaw: view.Docs["pi.inbox"],
		agentRaw: view.Docs["pi.agent"], usageRaw: view.Docs["pi.usage"],
	}
	if live, ok := decodeJSONInto[LiveState](parts.liveRaw); ok {
		parts.live = live
	}
	if parts.inboxRaw != nil {
		if inbox, err := decodeInboxState(parts.inboxRaw); err == nil {
			parts.inbox = &inbox
		}
	}
	if agent, ok := decodeJSONInto[AgentState](parts.agentRaw); ok {
		parts.agent = &agent
	}
	if usage, ok := decodeJSONInto[UsageState](parts.usageRaw); ok {
		parts.usage = &usage
	}
	return parts
}

func snapshotOf(view ConversationView) AgentEvent {
	parts := eventParts(view)
	event := AgentEvent{
		Type: EventSnapshot, Entries: view.Entries, Tools: parts.live.Tools,
		Compactions: parts.live.Compactions, Inbox: queuedItems(parts.inbox),
	}
	agent := agentOrEmpty(parts.agent)
	event.Agent = &agent
	usage := usageOrEmpty(parts.usage)
	event.Usage = &usage
	if event.Tools == nil {
		event.Tools = []ToolSlot{}
	}
	if event.Compactions == nil {
		event.Compactions = []CompactionStatus{}
	}
	if parts.live.Run != nil {
		event.Run = &RunInputs{Inputs: append([]Id{}, parts.live.Run.Inputs...)}
	}
	if parts.live.Generation != nil {
		event.Generation = snapshotGeneration(parts.live.Generation)
	}
	return event
}

func queuedItems(inbox *InboxState) []QueuedItem {
	items := []QueuedItem{}
	if inbox == nil {
		return items
	}
	for _, item := range inbox.Items {
		items = append(items, QueuedItem{ID: item.ID, Mode: item.Mode})
	}
	return items
}

func snapshotGeneration(generation *LiveGeneration) *SnapshotGeneration {
	return &SnapshotGeneration{
		Attempt: generation.Attempt, Message: assistantOf(generation.Message),
		Retry: generation.Retry, Deferred: generation.Deferred,
	}
}

func agentOrEmpty(agent *AgentState) AgentState {
	if agent == nil {
		return AgentState{}
	}
	return *agent
}

func usageOrEmpty(usage *UsageState) UsageState {
	if usage == nil {
		return UsageState{Models: map[string]ai.Usage{}, Tools: map[string]ai.Usage{}}
	}
	return *usage
}

// WatchEvents attaches to one conversation's agent events. The snapshot and
// the registration for later commits are captured atomically on the Session
// line; overflow replaces undelivered batches with one snapshot.
func WatchEvents(session *Session, conversationID Id, ctx chord.Context) (*AgentEventStream, error) {
	views, err := ConversationViewsFor(session)
	if err != nil {
		return nil, err
	}
	var watch *CommittedWatch[[]AgentEvent]
	var snapshot AgentEvent
	_, _, err = views.Attach(conversationID, func(initial ConversationView, release func(), storage Storage) (ViewObserver, error) {
		completing, err := ScanAll(func(cursor Cursor) (Page[TaskRecord], error) {
			kind := RunTaskKind
			status := TaskCompleting
			conversation := conversationID
			return storage.ScanTasks(ctx, TaskQuery{ConversationID: &conversation, Kind: &kind, Status: &status}, cursor, 100)
		})
		if err != nil {
			return nil, err
		}
		held := map[Id]bool{}
		for _, record := range completing {
			held[record.ID] = true
		}
		current := initial
		snapshot = snapshotOf(initial)
		watch = NewCommittedWatch[[]AgentEvent]([]AgentEvent{}, release, func() []AgentEvent {
			return []AgentEvent{snapshotOf(current)}
		})
		return &eventStreamObserver{
			publication: func(before, after ConversationView, ops []delta.Op, publication CommitPublication, commitContext chord.Context) {
				current = after
				events := Translate(conversationID, before, after, ops, publication, held)
				if len(events) > 0 {
					watch.Advance(events, nil, commitContext)
				}
			},
			closeSession: func() { watch.CloseSession() },
		}, nil
	}, ctx)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		watch.Cancel()
		return nil, ctx.Err()
	}
	watch.ObserveCancellation(ctx)
	return &AgentEventStream{
		Snapshot: snapshot,
		Start: func(listener AgentEventListener) {
			watch.Start(func(events []AgentEvent, _ []delta.Op, deliveryContext chord.Context) error {
				return listener(events, deliveryContext)
			})
		},
		Stop:   func() WatchEnd { return watch.Stop() },
		Closed: watch.Closed,
		End:    watch.End,
	}, nil
}

type eventStreamObserver struct {
	publication  func(before, after ConversationView, ops []delta.Op, publication CommitPublication, ctx chord.Context)
	closeSession func()
}

func (o *eventStreamObserver) CloseSession() { o.closeSession() }

func (o *eventStreamObserver) Publication(before, after ConversationView, ops []delta.Op, publication CommitPublication, ctx chord.Context) {
	o.publication(before, after, ops, publication, ctx)
}

// Translate is every event one publication causes, in the order of spec §9.4.
func Translate(
	conversationID Id,
	before, after ConversationView,
	viewOps []delta.Op,
	publication CommitPublication,
	held map[Id]bool,
) []AgentEvent {
	entries := []EntryRecord{}
	tasks := map[Id]TaskRecord{}
	submissions := []SubmissionRecord{}
	for _, change := range publication.Changes {
		if change.Write == nil {
			continue
		}
		switch change.Write.Type {
		case "entry":
			if entry := change.Write.Entry; entry != nil && entry.ConversationID == conversationID {
				entries = append(entries, *entry)
			}
		case "task":
			if record := change.Write.Task; record != nil && record.ConversationID == conversationID {
				tasks[record.ID] = *record
			}
		case "submission":
			if record := change.Write.Submission; record != nil && record.ConversationID == conversationID {
				submissions = append(submissions, *record)
			}
		}
	}
	if len(viewOps) == 0 && len(entries) == 0 && len(tasks) == 0 && len(submissions) == 0 {
		return nil
	}
	sort.SliceStable(submissions, func(left, right int) bool { return submissions[left].ID < submissions[right].ID })
	was := eventParts(before)
	now := eventParts(after)
	events := []AgentEvent{}

	slotsBefore := map[string]ToolSlot{}
	for _, slot := range was.live.Tools {
		slotsBefore[slot.CallID] = slot
	}
	slots := now.live.Tools
	for _, slot := range slots {
		if slot.Status != ToolSlotRunning {
			continue
		}
		if previous, present := slotsBefore[slot.CallID]; present && previous.Status == ToolSlotRunning {
			continue
		}
		args := JsonObject{}
		if slot.TaskID != nil {
			if record, present := tasks[*slot.TaskID]; present {
				args = checkpointArguments(record)
			}
		}
		events = append(events, AgentEvent{
			Type: EventToolExecutionStart, ToolCallID: slot.CallID, ToolName: slot.Name, Args: args,
		})
	}
	partialBefore := assistantFromGeneration(was.live.Generation)
	partial := assistantFromGeneration(now.live.Generation)
	partialRawBefore := generationMessageRaw(was.liveRaw)
	partialRaw := generationMessageRaw(now.liveRaw)
	switch {
	case partial != nil && partialBefore == nil:
		events = append(events, AgentEvent{Type: EventMessageStart, Message: partial})
	case partial != nil && !jsonDeepEqual(partialRaw, partialRawBefore):
		usage := partial.Usage
		events = append(events, AgentEvent{
			Type: EventMessageUpdate, UsageUpdate: &usage, Changes: messageChanges(viewOps, partial),
		})
	}
	for index, slot := range slots {
		previous, present := slotsBefore[slot.CallID]
		if slot.Status != ToolSlotRunning || !present || previous.Status != ToolSlotRunning {
			continue
		}
		update := toolUpdate(viewOps, index, slot, previous)
		if update == nil {
			continue
		}
		event := AgentEvent{Type: EventToolExecutionUpdate, ToolCallID: slot.CallID, ToolName: slot.Name, Output: update.output}
		if update.detailsSet {
			event.Details = update.details
		}
		if update.diagnosticsSet {
			event.Diagnostics = update.diagnostics
		}
		events = append(events, event)
	}
	generation := now.live.Generation
	generationBefore := was.live.Generation
	if generation != nil && generation.Retry != nil && (generationBefore == nil || generationBefore.Retry == nil) {
		events = append(events, AgentEvent{
			Type: EventAutoRetryStart, Attempt: generation.Attempt,
			At: generation.Retry.At, ErrorMessage: generation.Retry.Error,
		})
	}
	if generationBefore != nil && generationBefore.Retry != nil && (generation == nil || generation.Retry == nil) {
		events = append(events, AgentEvent{Type: EventAutoRetryEnd, Attempt: generationBefore.Attempt})
	}
	if generation != nil && generation.Deferred != nil &&
		(generationBefore == nil || generationBefore.Deferred == nil || generationBefore.Deferred.PollAt != generation.Deferred.PollAt) {
		events = append(events, AgentEvent{Type: EventDeferredPoll, PollAt: generation.Deferred.PollAt})
	}

	toolEnds := []AgentEvent{}
	endTool := func(callID, name string, entryID *Id) {
		event := AgentEvent{Type: EventToolExecutionEnd, ToolCallID: callID, ToolName: name}
		if entryID != nil {
			for index := range entries {
				if entries[index].ID == *entryID {
					event.Entry = &entries[index]
					break
				}
			}
		}
		toolEnds = append(toolEnds, event)
	}
	for _, previous := range slotsBefore {
		if previous.Status == ToolSlotDone {
			continue
		}
		var slot *ToolSlot
		for index := range slots {
			if slots[index].CallID == previous.CallID {
				slot = &slots[index]
				break
			}
		}
		switch {
		case slot != nil && slot.Status == ToolSlotDone:
			endTool(previous.CallID, previous.Name, slot.Entry)
		case slot == nil:
			endTool(previous.CallID, previous.Name, resultOf(entries, previous.CallID))
		}
	}
	for index := range slots {
		slot := slots[index]
		if slot.Status == ToolSlotDone {
			if _, present := slotsBefore[slot.CallID]; !present {
				endTool(slot.CallID, slot.Name, slot.Entry)
			}
		}
	}

	assistantAppended := false
	for index := range entries {
		entry := entries[index]
		for _, end := range toolEnds {
			if end.Entry != nil && end.Entry.ID == entry.ID {
				events = append(events, end)
			}
		}
		message := firstModelMessage(entry)
		if message == nil {
			events = append(events, AgentEvent{Type: EventEntryAppended, Entry: &entries[index]})
			continue
		}
		_, isAssistant := message.(*ai.AssistantMessage)
		streamed := isAssistant && partialBefore != nil && !assistantAppended
		if isAssistant {
			assistantAppended = true
		}
		if !streamed {
			events = append(events, AgentEvent{Type: EventMessageStart, Message: message})
		}
		events = append(events, AgentEvent{Type: EventMessageEnd, Entry: &entries[index]})
	}
	for _, end := range toolEnds {
		if end.Entry == nil {
			events = append(events, end)
		}
	}

	compactionsBefore := was.live.Compactions
	compactions := now.live.Compactions
	for _, status := range compactionsBefore {
		if !hasCompaction(compactions, status.TaskID) {
			events = append(events, AgentEvent{Type: EventCompactionEnd, TaskID: status.TaskID, Reason: status.Reason})
		}
	}
	turnEnded := false
	for _, task := range tasks {
		if task.Kind == RunTaskKind && task.State.Status == TaskCompleting && !held[task.ID] {
			held[task.ID] = true
			turnEnded = true
		}
		if task.State.Status != TaskTerminal {
			continue
		}
		if task.Kind == RunTaskKind && !held[task.ID] {
			turnEnded = true
		}
		delete(held, task.ID)
		outcome := task.State.Outcome
		if outcome != nil && (outcome.Status == OutcomeFaulted || outcome.Status == OutcomeOrphaned) {
			message := ""
			if outcome.Status == OutcomeFaulted && outcome.Error != nil {
				message = outcome.Error.Message
			} else if outcome.Reason != nil {
				message = *outcome.Reason
			}
			events = append(events, AgentEvent{Type: EventTaskFailed, TaskID: task.ID, Kind: task.Kind, ErrorMessage: message})
		}
	}
	if turnEnded {
		events = append(events, AgentEvent{Type: EventTurnEnd})
	}
	run := now.live.Run
	runBefore := was.live.Run
	runChanged := !runFirstEqual(run, runBefore)
	if runBefore != nil && runChanged {
		events = append(events, AgentEvent{Type: EventRunEnd, Inputs: append([]Id{}, runBefore.Inputs...)})
	}

	for index := range submissions {
		record := submissions[index]
		events = append(events, AgentEvent{Type: EventSubmission, Record: &record})
	}
	if !sameJSONValue(now.inboxRaw, was.inboxRaw) {
		events = append(events, AgentEvent{Type: EventInboxUpdate, Inbox: queuedItems(now.inbox)})
	}
	if !sameJSONValue(now.agentRaw, was.agentRaw) {
		agent := agentOrEmpty(now.agent)
		events = append(events, AgentEvent{Type: EventAgentChanged, Agent: &agent})
	}
	if !sameJSONValue(now.usageRaw, was.usageRaw) {
		usage := usageOrEmpty(now.usage)
		events = append(events, AgentEvent{Type: EventUsageChanged, Usage: &usage})
	}
	for _, status := range compactions {
		if !hasCompaction(compactionsBefore, status.TaskID) {
			events = append(events, AgentEvent{
				Type: EventCompactionStart, TaskID: status.TaskID, Reason: status.Reason, Blocking: status.Blocking,
			})
		}
	}
	if run != nil && runChanged {
		events = append(events, AgentEvent{Type: EventRunStart, Inputs: append([]Id{}, run.Inputs...)})
	}
	if run != nil && (runBefore == nil || run.TaskID != runBefore.TaskID) {
		if record, present := tasks[run.TaskID]; present && record.Kind == RunTaskKind {
			events = append(events, AgentEvent{Type: EventTurnStart})
		}
	}
	return events
}

var partialPath = delta.Path{"docs", "pi.live", "generation", "message"}

// messageChanges translates the view operations on the in-flight message into
// message changes (spec §9.4).
func messageChanges(viewOps []delta.Op, message *ai.AssistantMessage) []MessageChange {
	changes := []MessageChange{}
	whole := map[int]bool{}
	for _, op := range viewOps {
		path := op.Path
		if !pathStartsWith(path, partialPath) {
			if pathStartsWith(partialPath, path) {
				return []MessageChange{{Type: MessageWhole, Message: message}}
			}
			continue
		}
		rest := path[len(partialPath):]
		if len(rest) > 0 {
			if field, ok := rest[0].(string); ok && field == "usage" {
				continue
			}
		}
		if len(rest) == 0 || !segEquals(rest[0], "content") {
			return []MessageChange{{Type: MessageWhole, Message: message}}
		}
		if len(rest) == 1 {
			if op.Verb != delta.VerbSplice || op.Remove != 0 {
				return []MessageChange{{Type: MessageWhole, Message: message}}
			}
			for offset, item := range op.Items {
				block, ok := decodeContentBlock(item)
				if !ok {
					continue
				}
				changes = append(changes, MessageChange{
					Type: blockStartType(block), ContentIndex: op.Index + offset, Block: block,
				})
			}
			continue
		}
		contentIndex, ok := segInt(rest[1])
		if !ok {
			return []MessageChange{{Type: MessageWhole, Message: message}}
		}
		field, _ := rest[2].(string)
		if whole[contentIndex] {
			continue
		}
		if op.Verb == delta.VerbAppend && len(rest) == 3 && (field == "text" || field == "thinking") {
			changes = append(changes, MessageChange{
				Type: fieldDeltaType(field), ContentIndex: contentIndex, Delta: op.Text,
			})
			continue
		}
		if op.Verb == delta.VerbAppend && field == "arguments" {
			changes = append(changes, MessageChange{
				Type: MessageToolcallDelta, ContentIndex: contentIndex,
				Path: append([]any{}, rest[3:]...), Delta: op.Text,
			})
			continue
		}
		whole[contentIndex] = true
		if contentIndex < len(message.Content) {
			changes = append(changes, MessageChange{
				Type: MessageBlock, ContentIndex: contentIndex, Block: message.Content[contentIndex],
			})
		}
	}
	return changes
}

type toolUpdateResult struct {
	output         *ToolOutput
	details        chord.JsonValue
	detailsSet     bool
	diagnostics    []ToolDiagnostic
	diagnosticsSet bool
}

// toolUpdate is the output, details and diagnostics changes of a running slot.
func toolUpdate(viewOps []delta.Op, index int, slot, previous ToolSlot) *toolUpdateResult {
	outputPath := delta.Path{"docs", "pi.live", "tools", index, "output"}
	trimStart := 0
	appendText := ""
	set := false
	for _, op := range viewOps {
		if !pathStartsWith(op.Path, outputPath) {
			continue
		}
		switch op.Verb {
		case delta.VerbTruncate:
			trimStart += op.Count
		case delta.VerbAppend:
			appendText += op.Text
		default:
			set = true
		}
	}
	var output *ToolOutput
	switch {
	case set || (!sameOptionalString(slot.Output, previous.Output) && trimStart == 0 && appendText == ""):
		value := ""
		if slot.Output != nil {
			value = *slot.Output
		}
		output = &ToolOutput{Set: &value}
	case trimStart > 0 || appendText != "":
		output = &ToolOutput{}
		if trimStart > 0 {
			output.TrimStart = &trimStart
		}
		if appendText != "" {
			output.Append = &appendText
		}
	}
	result := &toolUpdateResult{output: output}
	if !sameJSONValue(slot.Details, previous.Details) {
		result.details = slot.Details
		result.detailsSet = true
	}
	if !reflect.DeepEqual(slot.Diagnostics, previous.Diagnostics) {
		diagnostics := slot.Diagnostics
		if diagnostics == nil {
			diagnostics = []ToolDiagnostic{}
		}
		result.diagnostics = diagnostics
		result.diagnosticsSet = true
	}
	if output == nil && !result.detailsSet && !result.diagnosticsSet {
		return nil
	}
	return result
}

// resultOf is the result entry for callID among entries.
func resultOf(entries []EntryRecord, callID string) *Id {
	for index := range entries {
		message := firstModelMessage(entries[index])
		if result, ok := message.(*ai.ToolResultMessage); ok && result.ToolCallID == callID {
			id := entries[index].ID
			return &id
		}
	}
	return nil
}

func checkpointArguments(record TaskRecord) JsonObject {
	if len(record.State.Checkpoint) == 0 {
		return JsonObject{}
	}
	var checkpoint map[string]any
	if err := json.Unmarshal(record.State.Checkpoint, &checkpoint); err != nil {
		return JsonObject{}
	}
	if args, ok := checkpoint["arguments"].(map[string]any); ok {
		return args
	}
	return JsonObject{}
}

func hasCompaction(statuses []CompactionStatus, taskID Id) bool {
	for _, status := range statuses {
		if status.TaskID == taskID {
			return true
		}
	}
	return false
}

func firstModelMessage(entry EntryRecord) ai.Message {
	if len(entry.Model) == 0 {
		return nil
	}
	return entry.Model[0]
}

func assistantOf(value chord.JsonValue) *ai.AssistantMessage {
	if value == nil {
		return nil
	}
	encoded, err := marshalJSONValue(value)
	if err != nil {
		return nil
	}
	message, err := ai.UnmarshalMessage(json.RawMessage(encoded))
	if err != nil {
		return nil
	}
	assistant, _ := message.(*ai.AssistantMessage)
	return assistant
}

func assistantFromGeneration(generation *LiveGeneration) *ai.AssistantMessage {
	if generation == nil {
		return nil
	}
	return assistantOf(generation.Message)
}

// generationMessageRaw reads the in-flight partial as JSON without decoding,
// for the identity comparison.
func generationMessageRaw(live chord.JsonValue) chord.JsonValue {
	object, ok := live.(map[string]any)
	if !ok {
		return nil
	}
	generation, ok := object["generation"].(map[string]any)
	if !ok {
		return nil
	}
	return generation["message"]
}

func decodeContentBlock(value chord.JsonValue) (ai.Content, bool) {
	encoded, err := marshalJSONValue(value)
	if err != nil {
		return nil, false
	}
	synthetic := make([]byte, 0, len(encoded)+32)
	synthetic = append(synthetic, `{"role":"assistant","content":[`...)
	synthetic = append(synthetic, encoded...)
	synthetic = append(synthetic, `]}`...)
	message, err := ai.UnmarshalMessage(json.RawMessage(synthetic))
	if err != nil {
		return nil, false
	}
	assistant, ok := message.(*ai.AssistantMessage)
	if !ok || len(assistant.Content) != 1 {
		return nil, false
	}
	return assistant.Content[0], true
}

func blockStartType(block ai.Content) string {
	switch block.(type) {
	case ai.TextContent:
		return MessageTextStart
	case ai.ThinkingContent:
		return MessageThinkingStart
	default:
		return MessageToolcallStart
	}
}

func fieldDeltaType(field string) string {
	if field == "thinking" {
		return MessageThinkingDelta
	}
	return MessageTextDelta
}

func pathStartsWith(path, prefix delta.Path) bool {
	if len(prefix) > len(path) {
		return false
	}
	for index := range prefix {
		if !segEquals(path[index], prefix[index]) {
			return false
		}
	}
	return true
}

func segEquals(segment any, target any) bool {
	if text, ok := target.(string); ok {
		value, ok := segment.(string)
		return ok && value == text
	}
	left, leftOK := segInt(segment)
	right, rightOK := segInt(target)
	return leftOK && rightOK && left == right
}

func segInt(segment any) (int, bool) {
	switch typed := segment.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}

func runFirstEqual(run, before *LiveRun) bool {
	var first, beforeFirst *Id
	if run != nil && len(run.Inputs) > 0 {
		first = &run.Inputs[0]
	}
	if before != nil && len(before.Inputs) > 0 {
		beforeFirst = &before.Inputs[0]
	}
	if first == nil || beforeFirst == nil {
		return first == nil && beforeFirst == nil
	}
	return *first == *beforeFirst
}

func jsonDeepEqual(left, right any) bool {
	return reflect.DeepEqual(left, right)
}

// sameJSONValue compares two document values by map identity, falling back to
// deep equality for scalars and non-map values.
func sameJSONValue(left, right any) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	leftValue, rightValue := reflect.ValueOf(left), reflect.ValueOf(right)
	if leftValue.Kind() == reflect.Map && rightValue.Kind() == reflect.Map {
		return leftValue.Pointer() == rightValue.Pointer()
	}
	return reflect.DeepEqual(left, right)
}

func decodeJSONInto[T any](value chord.JsonValue) (T, bool) {
	var out T
	if value == nil {
		return out, false
	}
	encoded, err := marshalJSONValue(value)
	if err != nil {
		return out, false
	}
	if err := json.Unmarshal([]byte(encoded), &out); err != nil {
		return out, false
	}
	return out, true
}
