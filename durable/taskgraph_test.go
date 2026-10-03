package durable

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
)

// Port of harness/task-graph.ts.

func taskDefinition(name string) TaskDefinition {
	return TaskDefinition{
		Name: name, Version: 1,
		Initial: func(input json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`{"phase":"start"}`), nil },
	}
}

// stagedTask finds a task candidate staged by this transaction.
func stagedTask(tx *Transaction, id Id) *TaskRecord {
	for _, record := range tx.StagedTasks() {
		if record.ID == id {
			return record
		}
	}
	return nil
}

func taskKey(id Id) string { return strconv.FormatInt(int64(id), 10) }

func TestTaskGraphViewBuildsAndAdvances(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	view := NewTaskGraphView(session, storage)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var pendingID Id
	// A pending task and a waiting task.
	if err := session.Commit(ctx, func(tx *Transaction) error {
		id, err := tx.CreateTask(taskDefinition("pending"), json.RawMessage(`{}`), TaskOptions{
			Ownership: TaskOwnership{Kind: TaskOwnedByConversation}, ConversationID: idPtr(RootConversationID),
		})
		if err != nil {
			return err
		}
		pendingID = id
		waitingID, err := tx.CreateTask(taskDefinition("waiting"), json.RawMessage(`{}`), TaskOptions{
			Ownership: TaskOwnership{Kind: TaskOwnedByConversation}, ConversationID: idPtr(RootConversationID),
		})
		if err != nil {
			return err
		}
		record := stagedTask(tx, waitingID)
		finished := *record
		finished.State = TaskState{Status: TaskWaiting, Checkpoint: json.RawMessage(`{"phase":"park"}`), On: []Id{pendingID}, Policy: JoinAllSettled}
		return tx.SetTask(&finished)
	}); err != nil {
		t.Fatal(err)
	}
	// A task that reaches terminal in the same commit never enters the graph.
	if err := session.Commit(ctx, func(tx *Transaction) error {
		id, err := tx.CreateTask(taskDefinition("terminal"), json.RawMessage(`{}`), TaskOptions{
			Ownership: TaskOwnership{Kind: TaskOwnedByConversation}, ConversationID: idPtr(RootConversationID),
		})
		if err != nil {
			return err
		}
		record := stagedTask(tx, id)
		finished := *record
		finished.State = TaskState{Status: TaskTerminal, Outcome: &TaskOutcome{Status: OutcomeCompleted, Result: json.RawMessage(`1`)}}
		return tx.SetTask(&finished)
	}); err != nil {
		t.Fatal(err)
	}

	state, detach, err := view.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	value, ok := state.Value()
	if !ok {
		t.Fatal("no graph value")
	}
	if len(value.Tasks) != 2 {
		t.Fatalf("tasks = %+v", value.Tasks)
	}
	nodes := map[string]TaskGraphNode{}
	for _, node := range value.Tasks {
		nodes[node.Kind] = node
	}
	if nodes["pending"].State.Status != TaskPending || nodes["pending"].State.Phase != "start" {
		t.Fatalf("pending = %+v", nodes["pending"])
	}
	if nodes["waiting"].State.Status != TaskWaiting || len(nodes["waiting"].State.On) != 1 ||
		nodes["waiting"].State.Policy != JoinAllSettled || nodes["waiting"].State.Phase != "park" {
		t.Fatalf("waiting = %+v", nodes["waiting"])
	}
	if _, present := nodes["terminal"]; present {
		t.Fatalf("terminal task stayed: %+v", value.Tasks)
	}

	// A conversation created with its owner task appears in that task's node.
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateConversation(ConversationOwnership{Kind: ConversationOwnedByTask, TaskID: &pendingID})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	value, _ = state.Value()
	owner := value.Tasks[taskKey(pendingID)]
	if len(owner.Conversations) != 1 || owner.Conversations[0] == RootConversationID {
		t.Fatalf("owned conversations = %+v", owner.Conversations)
	}

	// Finishing a live task removes it from the graph.
	if err := session.Commit(ctx, func(tx *Transaction) error {
		record, err := tx.Task(pendingID)
		if err != nil {
			return err
		}
		finished := *record
		finished.State = TaskState{Status: TaskTerminal, Outcome: &TaskOutcome{Status: OutcomeCompleted, Result: json.RawMessage(`1`)}}
		return tx.SetTask(&finished)
	}); err != nil {
		t.Fatal(err)
	}
	value, _ = state.Value()
	if _, present := value.Tasks[taskKey(pendingID)]; present {
		t.Fatalf("finished task stayed: %+v", value.Tasks)
	}
}

func TestTaskGraphViewWatch(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	view := NewTaskGraphView(session, storage)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	watch, detach, err := view.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	updated := make(chan struct{}, 1)
	watch.Start(func(value TaskGraph, ops []delta.Op, _ chord.Context) error {
		select {
		case updated <- struct{}{}:
		default:
		}
		return nil
	})
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateTask(taskDefinition("watched"), json.RawMessage(`{}`), TaskOptions{
			Ownership: TaskOwnership{Kind: TaskOwnedByConversation}, ConversationID: idPtr(RootConversationID),
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-updated:
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not advance")
	}
	watch.Stop()
}
