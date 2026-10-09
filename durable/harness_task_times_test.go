package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// Port of task lifecycle timing from
// packages/durable/src/session/transaction.ts at pi v1.1.0 commit 36a686ee8.
func TestTaskStartedAtStampsFirstRunAndSurvivesWaits(t *testing.T) {
	clock := int64(1_000)
	session, storage := openTaskTimingSession(t, func() int64 { return clock })
	taskID := createTaskForTiming(t, session)
	ctx := context.Background()
	pending, err := storage.Task(ctx, taskID)
	if err != nil || pending == nil || pending.StartedAt != nil || pending.EndedAt != nil {
		t.Fatalf("pending task times = %+v, %v", pending, err)
	}

	clock = 2_000
	setTaskTimingState(t, session, taskID, TaskRunning)
	started, err := storage.Task(ctx, taskID)
	if err != nil || started == nil || started.StartedAt == nil || *started.StartedAt != 2_000 || started.EndedAt != nil {
		t.Fatalf("running task times = %+v, %v", started, err)
	}

	clock = 3_000
	setTaskTimingState(t, session, taskID, TaskWaiting)
	clock = 4_000
	setTaskTimingState(t, session, taskID, TaskRunning)
	resumed, err := storage.Task(ctx, taskID)
	if err != nil || resumed == nil || resumed.StartedAt == nil || *resumed.StartedAt != 2_000 || resumed.EndedAt != nil {
		t.Fatalf("resumed task times = %+v, %v", resumed, err)
	}
}

// Port of terminal lifecycle stamping from
// packages/durable/src/session/transaction.ts at pi v1.1.0 commit 36a686ee8.
func TestTaskEndedAtStampsTerminalAndDoesNotInventStart(t *testing.T) {
	clock := int64(1_000)
	session, storage := openTaskTimingSession(t, func() int64 { return clock })
	runTask := createTaskForTiming(t, session)
	clock = 2_000
	setTaskTimingState(t, session, runTask, TaskRunning)
	running, err := storage.Task(context.Background(), runTask)
	if err != nil || running == nil || running.State.Status != TaskRunning {
		t.Fatalf("before terminal transition = %+v, %v", running, err)
	}
	clock = 3_000
	setTaskTimingState(t, session, runTask, TaskCompleting)
	completing, err := storage.Task(context.Background(), runTask)
	if err != nil || completing == nil || completing.StartedAt == nil || *completing.StartedAt != 2_000 || completing.EndedAt != nil {
		t.Fatalf("completing task times = %+v, %v", completing, err)
	}
	clock = 4_000
	setTaskTimingState(t, session, runTask, TaskTerminal)
	settled, err := storage.Task(context.Background(), runTask)
	if err != nil || settled == nil || settled.StartedAt == nil || *settled.StartedAt != 2_000 ||
		settled.EndedAt == nil || *settled.EndedAt != 4_000 {
		t.Fatalf("settled task times = %+v, %v", settled, err)
	}

	neverRun := createTaskForTiming(t, session)
	clock = 5_000
	setTaskTimingState(t, session, neverRun, TaskTerminal)
	orphaned, err := storage.Task(context.Background(), neverRun)
	if err != nil || orphaned == nil || orphaned.StartedAt != nil || orphaned.EndedAt == nil || *orphaned.EndedAt != 5_000 {
		t.Fatalf("never-run terminal times = %+v, %v", orphaned, err)
	}
}

// Port of the Date.now default in packages/durable/src/harness/harness.ts and
// session/session.ts at pi v1.1.0 commit 36a686ee8.
func TestHarnessDefaultsTaskClockToWallTime(t *testing.T) {
	session, storage := openTaskTimingSession(t, nil)
	taskID := createTaskForTiming(t, session)
	before := time.Now().UnixMilli()
	setTaskTimingState(t, session, taskID, TaskRunning)
	after := time.Now().UnixMilli()
	record, err := storage.Task(context.Background(), taskID)
	if err != nil || record == nil || record.StartedAt == nil || *record.StartedAt < before || *record.StartedAt > after {
		t.Fatalf("default startedAt = %+v, want [%d, %d], err=%v", record, before, after, err)
	}
}

// Port of createSession's Date.now default from packages/durable/src/session/session.ts at pi v1.1.0 commit 36a686ee8.
func TestSessionDefaultsTaskClockToWallTime(t *testing.T) {
	storage := NewMemoryStorage()
	session := NewSession(storage)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := session.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	taskID := createTaskForTiming(t, session)
	before := time.Now().UnixMilli()
	setTaskTimingState(t, session, taskID, TaskRunning)
	after := time.Now().UnixMilli()
	record, err := storage.Task(ctx, taskID)
	if err != nil || record == nil || record.StartedAt == nil || *record.StartedAt < before || *record.StartedAt > after {
		t.Fatalf("default session startedAt = %+v, want [%d, %d], err=%v", record, before, after, err)
	}
}

// Port of candidate timestamp preservation from
// packages/durable/src/session/transaction.ts at pi v1.1.0 commit 36a686ee8.
func TestTaskTimesCarryAcrossCandidateReplacements(t *testing.T) {
	clock := int64(1_000)
	session, storage := openTaskTimingSession(t, func() int64 { return clock })
	taskID := createTaskForTiming(t, session)
	clock = 2_000
	if err := session.Commit(context.Background(), func(tx *Transaction) error {
		current, err := tx.Task(taskID)
		if err != nil {
			return err
		}
		current = cloneTask(current)
		current.State = TaskState{Status: TaskRunning, Checkpoint: current.State.Checkpoint}
		if err := tx.SetTask(current); err != nil {
			return err
		}
		clock = 3_000
		terminal := cloneTask(tx.StagedTasks()[0])
		terminal.State = TaskState{Status: TaskTerminal, Outcome: &TaskOutcome{Status: OutcomeCompleted}}
		terminal.StartedAt, terminal.EndedAt = nil, nil
		return tx.SetTask(terminal)
	}); err != nil {
		t.Fatal(err)
	}
	settled, err := storage.Task(context.Background(), taskID)
	if err != nil || settled == nil || settled.StartedAt == nil || *settled.StartedAt != 2_000 ||
		settled.EndedAt == nil || *settled.EndedAt != 3_000 {
		t.Fatalf("candidate replacement times = %+v, %v", settled, err)
	}
}

// Port of task time recovery from
// packages/durable/test/harness-tasks-recovery.test.ts at pi v1.1.0 commit 36a686ee8.
func TestTaskTimesSurviveSqliteSessionReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tasks.db")
	clock := int64(2_000)
	firstStorage, err := OpenSqliteStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	first := NewSessionWithClock(firstStorage, func() int64 { return clock })
	t.Cleanup(func() {
		if err := first.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	if err := first.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	taskID := createTaskForTiming(t, first)
	setTaskTimingState(t, first, taskID, TaskRunning)
	if err := first.Close(ctx); err != nil {
		t.Fatal(err)
	}

	clock = 9_000
	secondStorage, err := OpenSqliteStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	second := NewSessionWithClock(secondStorage, func() int64 { return clock })
	t.Cleanup(func() {
		if err := second.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	reopened, err := secondStorage.Task(ctx, taskID)
	if err != nil || reopened == nil || reopened.StartedAt == nil || *reopened.StartedAt != 2_000 || reopened.EndedAt != nil {
		t.Fatalf("reopened task times = %+v, %v", reopened, err)
	}
	setTaskTimingState(t, second, taskID, TaskTerminal)
	settled, err := secondStorage.Task(ctx, taskID)
	if err != nil || settled == nil || settled.StartedAt == nil || *settled.StartedAt != 2_000 ||
		settled.EndedAt == nil || *settled.EndedAt != 9_000 {
		t.Fatalf("reopened terminal task times = %+v, %v", settled, err)
	}
}

func openTaskTimingSession(t *testing.T, now func() int64) (*Session, Storage) {
	t.Helper()
	ctx := context.Background()
	storage := NewMemoryStorage()
	harness := NewHarness(storage, HarnessOptions{Now: now}, ctx)
	session := harness.Session
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := session.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return session, storage
}

func createTaskForTiming(t *testing.T, session *Session) Id {
	t.Helper()
	definition := TaskDefinition{
		Name: "test.task-times", Version: 1,
		Initial: func(json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`{"phase":"start"}`), nil },
	}
	var taskID Id
	if err := session.Commit(context.Background(), func(tx *Transaction) error {
		var err error
		taskID, err = tx.CreateTask(definition, json.RawMessage(`{}`), TaskOptions{
			Ownership: TaskOwnership{Kind: TaskOwnedByConversation}, ConversationID: idPointerOf(RootConversationID),
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return taskID
}

func setTaskTimingState(t *testing.T, session *Session, taskID Id, status string) {
	t.Helper()
	if err := session.Commit(context.Background(), func(tx *Transaction) error {
		record, err := tx.Task(taskID)
		if err != nil {
			return err
		}
		if record == nil {
			return fmt.Errorf("task %d does not exist", taskID)
		}
		record = cloneTask(record)
		record.State = TaskState{Status: status, Checkpoint: record.State.Checkpoint}
		if status == TaskCompleting || status == TaskTerminal {
			record.State.Outcome = &TaskOutcome{Status: OutcomeCompleted}
		}
		return tx.SetTask(record)
	}); err != nil {
		t.Fatal(err)
	}
}
