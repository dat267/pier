package durable

import (
	"encoding/json"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
)

// Port of harness/live.ts: the built-in live conversation state document and
// the cleanup the scheduler runs for outcomes it writes itself.

// Tool slot statuses.
const (
	ToolSlotPending = "pending"
	ToolSlotRunning = "running"
	ToolSlotDone    = "done"
)

// LiveRetry is a durable backoff before a retry.
type LiveRetry struct {
	At    int64  `json:"at"`
	Error string `json:"error"`
}

// LiveDeferred is a provider-side deferred response being polled.
type LiveDeferred struct {
	PollAt int64 `json:"pollAt"`
}

// LiveGeneration is the presentation of the current generation attempt.
type LiveGeneration struct {
	Attempt  int             `json:"attempt"`
	Message  chord.JsonValue `json:"message,omitempty"`
	Retry    *LiveRetry      `json:"retry,omitempty"`
	Deferred *LiveDeferred   `json:"deferred,omitempty"`
}

// ToolSlot is the presentation of one tool call of the current round.
type ToolSlot struct {
	CallID string `json:"callId"`
	Name   string `json:"name"`
	// TaskID is absent for a call not started yet and for one its request did
	// not offer (which starts done with the entry generation wrote).
	TaskID *Id `json:"taskId,omitempty"`
	// Status is pending/running/done.
	Status string `json:"status"`
	// Output and the dropped totals are retained running output.
	Output       *string `json:"output,omitempty"`
	DroppedBytes *int    `json:"droppedBytes,omitempty"`
	DroppedLines *int    `json:"droppedLines,omitempty"`
	// Details is the last `details()` value.
	Details chord.JsonValue `json:"details,omitempty"`
	// Diagnostics are recorded through `api.diagnostic()`.
	Diagnostics []ToolDiagnostic `json:"diagnostics,omitempty"`
	// Entry is the result entry once done; absent when the task faulted.
	Entry *Id `json:"entry,omitempty"`
}

// CompactionStatus is the presentation of one live compaction task.
type CompactionStatus struct {
	TaskID   Id               `json:"taskId"`
	Reason   CompactionReason `json:"reason"`
	Blocking bool             `json:"blocking"`
	Attempt  int              `json:"attempt"`
	Retry    *LiveRetry       `json:"retry,omitempty"`
}

// LiveRun is the run control of the current generation.
type LiveRun struct {
	TaskID Id   `json:"taskId"`
	Inputs []Id `json:"inputs"`
}

// LiveState is the built-in live conversation state: run control and
// presentation of the current generation and tool round.
type LiveState struct {
	Run         *LiveRun           `json:"run,omitempty"`
	Generation  *LiveGeneration    `json:"generation,omitempty"`
	Tools       []ToolSlot         `json:"tools,omitempty"`
	Compactions []CompactionStatus `json:"compactions,omitempty"`
}

// LiveDoc is the conversation-scoped live document (latest history, initial
// fork); it checkpoints whenever nothing runs.
var LiveDoc = mustDefineDoc(DocDefinition{
	Kind: "pi.live", Version: 1, Scope: ScopeConversation,
	History: stringPointer(HistoryLatest), Fork: stringPointer(ForkInitial),
	Initial: func(chord.JsonValue) (chord.JsonValue, error) { return map[string]any{}, nil },
	CheckpointWhen: func(value chord.JsonValue, _ []delta.Op, _ CheckpointInfo) bool {
		object, ok := value.(map[string]any)
		if !ok {
			return true
		}
		if _, present := object["generation"]; present {
			return false
		}
		tools, _ := object["tools"].([]any)
		for _, item := range tools {
			if slot, ok := item.(map[string]any); ok && slot["status"] == ToolSlotRunning {
				return false
			}
		}
		return true
	},
})

// Task kinds that can own a live run.
const (
	RunTaskKind        = "pi.generation"
	ToolTaskKind       = "pi.tool"
	CompactionTaskKind = "pi.compaction"
)

// SchedulerOutcome is a terminal outcome the scheduler writes itself.
type SchedulerOutcome struct {
	// Status is "faulted" or "orphaned".
	Status string
	Error  *StoredError
	Reason *string
}

// EndRun ends the run owned by taskID: settles each of its inputs and removes
// run, generation and tools.
func EndRun(tx *Transaction, live chord.JsonValue, taskID Id, settlement SubmissionSettlement) error {
	object, ok := live.(map[string]any)
	if !ok {
		return nil
	}
	if run, isRun := object["run"].(map[string]any); isRun && jsonIDEquals(run["taskId"], taskID) {
		if inputs, isList := run["inputs"].([]any); isList {
			for _, input := range inputs {
				if id, isID := jsonID(input); isID {
					if err := tx.SettleSubmission(id, settlement); err != nil {
						return err
					}
				}
			}
		}
		delete(object, "run")
	}
	delete(object, "generation")
	delete(object, "tools")
	return nil
}

// AddCompactionStatus appends a compaction task's status.
func AddCompactionStatus(live chord.JsonValue, status CompactionStatus) error {
	object, ok := live.(map[string]any)
	if !ok {
		return nil
	}
	value, err := jsonValueOf(status)
	if err != nil {
		return err
	}
	compactions, _ := object["compactions"].([]any)
	object["compactions"] = append(compactions, value)
	return nil
}

// CompactionStatusOf is the status of compaction task taskID, if listed.
func CompactionStatusOf(live chord.JsonValue, taskID Id) map[string]any {
	object, ok := live.(map[string]any)
	if !ok {
		return nil
	}
	compactions, _ := object["compactions"].([]any)
	for _, item := range compactions {
		if status, ok := item.(map[string]any); ok && jsonIDEquals(status["taskId"], taskID) {
			return status
		}
	}
	return nil
}

// RemoveCompactionStatus removes the status of compaction task taskID, and the
// list once empty.
func RemoveCompactionStatus(live chord.JsonValue, taskID Id) {
	object, ok := live.(map[string]any)
	if !ok {
		return
	}
	compactions, _ := object["compactions"].([]any)
	for index, item := range compactions {
		if status, ok := item.(map[string]any); ok && jsonIDEquals(status["taskId"], taskID) {
			compactions = append(compactions[:index], compactions[index+1:]...)
			break
		}
	}
	if len(compactions) == 0 {
		delete(object, "compactions")
		return
	}
	object["compactions"] = compactions
}

// ToolSlotOf is the slot of tool task taskID in the current round.
func ToolSlotOf(live chord.JsonValue, taskID Id) map[string]any {
	object, ok := live.(map[string]any)
	if !ok {
		return nil
	}
	tools, _ := object["tools"].([]any)
	for _, item := range tools {
		if slot, ok := item.(map[string]any); ok && jsonIDEquals(slot["taskId"], taskID) {
			return slot
		}
	}
	return nil
}

// FinishSlot marks a slot done: the result entry, if any, now carries its
// running output, details and diagnostics.
func FinishSlot(slot map[string]any, entry *Id) {
	slot["status"] = ToolSlotDone
	if entry != nil {
		slot["entry"] = *entry
	}
	ClearProgress(slot)
}

// ClearProgress removes what a tool published while running.
func ClearProgress(slot map[string]any) {
	for _, key := range []string{"output", "droppedBytes", "droppedLines", "details", "diagnostics"} {
		delete(slot, key)
	}
}

// ConvertPartial commits a live partial as an aborted assistant entry, keeping
// what the model produced and its usage.
func ConvertPartial(tx *Transaction, live chord.JsonValue, conversationID Id) error {
	object, ok := live.(map[string]any)
	if !ok {
		return nil
	}
	generation, ok := object["generation"].(map[string]any)
	if !ok {
		return nil
	}
	partial, present := generation["message"]
	if !present || partial == nil {
		return nil
	}
	message, ok := deepCopyJSON(partial).(map[string]any)
	if !ok {
		return nil
	}
	message["stopReason"] = ai.StopAborted
	encoded, err := marshalJSONValue(message)
	if err != nil {
		return err
	}
	decoded, err := ai.UnmarshalMessage(json.RawMessage(encoded))
	if err != nil {
		return err
	}
	_, err = tx.AppendEntry(conversationID, EntryDraft{Kind: AssistantEntry.Kind, Model: []ai.Message{decoded}})
	return err
}

// SettleSchedulerOutcome is the harness cleanup for a terminal outcome the
// scheduler writes itself (faulted or orphaned).
func SettleSchedulerOutcome(tx *Transaction, record TaskRecord, outcome SchedulerOutcome) error {
	switch record.Kind {
	case ToolTaskKind:
		live, err := tx.Doc(LiveDoc.Definition, record.ConversationID)
		if err != nil {
			return err
		}
		if slot := ToolSlotOf(live, record.ID); slot != nil {
			FinishSlot(slot, nil)
		}
		return nil
	case CompactionTaskKind:
		live, err := tx.Doc(LiveDoc.Definition, record.ConversationID)
		if err != nil {
			return err
		}
		RemoveCompactionStatus(live, record.ID)
		return nil
	case RunTaskKind:
		live, err := tx.Doc(LiveDoc.Definition, record.ConversationID)
		if err != nil {
			return err
		}
		object, ok := live.(map[string]any)
		if !ok {
			return nil
		}
		run, ok := object["run"].(map[string]any)
		if !ok || !jsonIDEquals(run["taskId"], record.ID) {
			return nil
		}
		if err := ConvertPartial(tx, live, record.ConversationID); err != nil {
			return err
		}
		settlement := SubmissionSettlement{Status: SubmissionUnanswered}
		if outcome.Status == OutcomeFaulted {
			reason := OutcomeFaulted
			settlement.Reason = &reason
			if outcome.Error != nil {
				detail, _ := json.Marshal(outcome.Error.Message)
				settlement.Detail = detail
			}
		} else {
			settlement.Reason = outcome.Reason
		}
		return EndRun(tx, live, record.ID, settlement)
	default:
		return nil
	}
}

// jsonValueOf converts a typed value through JSON.
func jsonValueOf(value any) (any, error) {
	encoded, err := marshalJSONValue(value)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

// jsonID converts a JSON number to an id.
func jsonID(value any) (Id, bool) {
	switch typed := value.(type) {
	case int64:
		return Id(typed), true
	case int:
		return Id(typed), true
	case float64:
		return Id(typed), true
	default:
		return 0, false
	}
}

// jsonIDEquals compares a JSON field to an id.
func jsonIDEquals(value any, id Id) bool {
	converted, ok := jsonID(value)
	return ok && converted == id
}
