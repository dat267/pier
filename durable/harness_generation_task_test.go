package durable

import (
	"context"
	"encoding/json"
	"testing"
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
