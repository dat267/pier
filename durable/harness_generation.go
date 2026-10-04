package durable

import (
	"encoding/json"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of harness/generation.ts: the generation task definition and the run
// lifecycle helpers. The phases land with the task runtime.

// Generation checkpoint phases.
const (
	GenerationPhasePrepare = "prepare"
	GenerationPhaseRequest = "request"
	GenerationPhaseRetry   = "retry"
	GenerationPhasePoll    = "poll"
	GenerationPhaseTools   = "tools"
)

// GenerationCheckpoint is the generation task's durable checkpoint.
type GenerationCheckpoint struct {
	Phase string `json:"phase"`
	// Attempt counts the request attempts.
	Attempt int `json:"attempt,omitempty"`
	// Compacted is the blocking compaction this generation waited for.
	Compacted *Id `json:"compacted,omitempty"`
	// Overflow is the overflow text checked once when prepare resumes.
	Overflow *string `json:"overflow,omitempty"`
	// Request phase.
	Model         *ModelRef                 `json:"model,omitempty"`
	ThinkingLevel string                    `json:"thinkingLevel,omitempty"`
	StreamOptions ConversationStreamOptions `json:"streamOptions,omitempty"`
	Cutoff        *Id                       `json:"cutoff,omitempty"`
	// Retry phase.
	Until *int64 `json:"until,omitempty"`
	// Poll phase.
	Handle *ai.DeferredHandle `json:"handle,omitempty"`
	PollAt *int64             `json:"pollAt,omitempty"`
	// Tools phase.
	Assistant Id       `json:"assistant,omitempty"`
	Tools     []Id     `json:"tools,omitempty"`
	Pending   []string `json:"pending,omitempty"`
}

// GenerationTask is the built-in generation task definition.
var GenerationTask Task

func init() {
	GenerationTask = Task{Definition: TaskDefinition{
		Name: RunTaskKind, Version: 1,
		Initial: func(json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"phase":"prepare","attempt":1}`), nil
		},
		Phases: map[string]PhaseHandler{
			GenerationPhasePrepare: generationPrepare,
			GenerationPhaseRequest: generationRequest,
			GenerationPhaseRetry:   generationRetry,
			GenerationPhasePoll:    generationPoll,
			GenerationPhaseTools:   generationTools,
		},
		Abort: generationAbort,
	}}
}

// StartRun creates a conversation-owned generation task and sets it as the
// run control of `live`.
func StartRun(tx *Transaction, conversationID Id, live chord.JsonValue, inputs []Id) error {
	taskID, err := CreateGeneration(tx, conversationID)
	if err != nil {
		return err
	}
	object, ok := live.(map[string]any)
	if !ok {
		return nil
	}
	object["run"] = map[string]any{"taskId": taskID, "inputs": idListValue(inputs)}
	return nil
}

// CreateGeneration creates a generation task owned by its conversation.
func CreateGeneration(tx *Transaction, conversationID Id) (Id, error) {
	return tx.CreateTask(GenerationTask.Definition, json.RawMessage(`{}`),
		TaskOptions{Ownership: TaskOwnership{Kind: TaskOwnedByConversation}, ConversationID: &conversationID})
}

// CreateToolTask creates a tool task for call callID, owned by the
// generation.
func CreateToolTask(tx *Transaction, owner Id, assistant Id, callID string) (Id, error) {
	encoded, err := marshalJSONValue(ToolTaskInput{Assistant: assistant, CallID: callID})
	if err != nil {
		return 0, err
	}
	return tx.CreateTask(ToolTask.Definition, json.RawMessage(encoded),
		TaskOptions{Ownership: TaskOwnership{Kind: TaskOwnedByTask, TaskID: &owner}})
}

// HandOver hands run control from one task to another; the run's inputs move
// with it.
func HandOver(live chord.JsonValue, from, to Id) {
	object, ok := live.(map[string]any)
	if !ok {
		return
	}
	run, ok := object["run"].(map[string]any)
	if !ok {
		return
	}
	if jsonIDEquals(run["taskId"], from) {
		run["taskId"] = to
	}
}

func idListValue(ids []Id) []any {
	values := make([]any, len(ids))
	for index, id := range ids {
		values[index] = id
	}
	return values
}
