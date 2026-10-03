package durable

import (
	"encoding/json"

	"github.com/dat267/pier/chord"
)

// Port of harness/generation.ts: the generation task definition and the run
// lifecycle helpers. The phases land with the task runtime.

// GenerationInput is the empty generation task input.
type GenerationInput = struct{}

// GenerationResult is the generation task result: the committed answer entry.
type GenerationResult struct {
	EntryID Id `json:"entryId"`
}

// GenerationTask is the built-in generation task definition.
var GenerationTask = Task{Definition: TaskDefinition{
	Name: RunTaskKind, Version: 1,
	Initial: func(json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"phase":"prepare","attempt":1}`), nil
	},
}}

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
