package durable

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

// Port of the built-in generation task phases.

func TestGenerationPrepareWithoutModelFails(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	var taskID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		taskID, err = CreateGeneration(tx, RootConversationID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	scheduler := newTestScheduler(t, session, storage)
	scheduler.Resume()
	settled, err := scheduler.WaitForTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.State.Status != TaskTerminal || settled.State.Outcome == nil ||
		settled.State.Outcome.Status != OutcomeFailed || settled.State.Outcome.Error == nil {
		t.Fatalf("settled = %+v", settled)
	}
	if settled.State.Outcome.Error.Message != "No model is configured" {
		t.Fatalf("error = %+v", settled.State.Outcome.Error)
	}
	var detail map[string]any
	if err := json.Unmarshal(settled.State.Outcome.Error.Detail, &detail); err != nil || detail["reason"] != "no_model" {
		t.Fatalf("detail = %s", settled.State.Outcome.Error.Detail)
	}
}

func TestGenerationFailsWhenBlockingThresholdCompactionFails(t *testing.T) {
	ctx := context.Background()
	session, storage := newRootSession(t)
	model := &ai.Model{ID: "m", API: ai.APIOpenAICompletions, Provider: "mock", ContextWindow: 1000, MaxTokens: 100}
	models := ai.CreateModels(&ai.CreateModelsOptions{})
	models.SetProvider(ai.CreateProvider(ai.CreateProviderOptions{
		ID: "mock", Name: "Mock", Auth: ai.ProviderAuth{APIKey: &ai.ApiKeyAuth{}}, Models: []*ai.Model{model},
	}))
	compactionDefinition := TaskDefinition{
		Name: "test.compaction", Version: 1,
		Initial: func(json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`{}`), nil },
	}
	var compactionID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		compactionID, err = tx.CreateTask(compactionDefinition, json.RawMessage(`{}`), TaskOptions{
			Ownership: TaskOwnership{Kind: TaskOwnedByConversation}, ConversationID: idPointerOf(RootConversationID),
		})
		if err != nil {
			return err
		}
		compaction := TaskRecord{
			ID: compactionID, ConversationID: RootConversationID, Kind: compactionDefinition.Name,
			Version: compactionDefinition.Version, Input: json.RawMessage(`{}`),
			State: TaskState{Status: TaskPending, Checkpoint: json.RawMessage(`{}`)},
		}
		return CommitTaskState(tx, nil, compaction, NextTaskState{
			Status: TaskTerminal, Outcome: &TaskOutcome{Status: OutcomeFailed, Error: &StoredError{Message: "summary provider failed"}},
		}, nil)
	}); err != nil {
		t.Fatal(err)
	}
	var generationID Id
	checkpoint := json.RawMessage(`{"phase":"prepare","compacted":` + itoaID(compactionID) + `,"threshold":true}`)
	generationDefinition := GenerationTask.Definition
	generationDefinition.Initial = func(json.RawMessage) (json.RawMessage, error) { return checkpoint, nil }
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		generationID, err = tx.CreateTask(generationDefinition, json.RawMessage(`{}`), TaskOptions{
			Ownership: TaskOwnership{Kind: TaskOwnedByConversation}, ConversationID: idPointerOf(RootConversationID),
		})
		if err != nil {
			return err
		}
		live, err := tx.Doc(LiveDoc.Definition, RootConversationID)
		if err != nil {
			return err
		}
		AssignJSON(live.(map[string]any), "run", map[string]any{"taskId": generationID, "inputs": []any{}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err := registry.Install(Extension{Name: "test"}); err != nil {
		t.Fatal(err)
	}
	scheduler := NewTaskScheduler(TaskSchedulerOptions{
		Session: session, Storage: storage, Registry: registry, Models: models,
		Agent: func(Id, RegistrySnapshot, context.Context) (Agent, error) {
			return Agent{Model: &ModelRef{Provider: "mock", ModelID: "m"}}, nil
		}, Context: ctx,
	})
	if err := scheduler.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer scheduler.Seal()
	scheduler.Resume()
	settled, err := scheduler.WaitForTask(ctx, generationID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.State.Outcome == nil || settled.State.Outcome.Error == nil ||
		settled.State.Outcome.Error.Message != "Compaction failed: summary provider failed" {
		t.Fatalf("generation outcome = %+v", settled.State.Outcome)
	}
}

func TestGenerationAbortSettlesAborted(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	var taskID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		taskID, err = CreateGeneration(tx, RootConversationID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	record, err := storage.Task(ctx, taskID)
	if err != nil || record == nil {
		t.Fatalf("task = %+v, %v", record, err)
	}
	record.AbortRequested = true
	if err := session.Commit(ctx, func(tx *Transaction) error { return tx.SetTask(record) }); err != nil {
		t.Fatal(err)
	}
	scheduler := newTestScheduler(t, session, storage)
	scheduler.Resume()
	settled, err := scheduler.WaitForTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.State.Status != TaskTerminal || settled.State.Outcome == nil ||
		settled.State.Outcome.Status != OutcomeAborted {
		t.Fatalf("settled = %+v", settled)
	}
}
