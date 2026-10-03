package durable

import (
	"context"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of the executable task surface in src/types.ts and code+tasks.ts: the
// runtime one task invocation sees, the phase and abort handlers, and the
// definition constructor. Upstream's generics are erased.

// RunningTask is a live task record reserved by one invocation.
type RunningTask struct {
	TaskRecord
}

// NextTaskState is the next state a task commits for itself: a replacement
// checkpoint, a wait, or its outcome.
type NextTaskState struct {
	// Status is running, waiting or terminal.
	Status string
	// Checkpoint replaces the checkpoint for a running state.
	Checkpoint []byte
	// On and Policy apply to a waiting state.
	On     []Id
	Policy string
	// Outcome applies to a terminal state.
	Outcome *TaskOutcome
}

// PhaseHandler runs one checkpoint phase. It must commit a changed checkpoint
// or a terminal outcome through the runtime; returning without durable progress
// faults the task.
type PhaseHandler func(task RunningTask, runtime TaskRuntime, ctx chord.Context) error

// HookRunner dispatches one hook of a task to every matching registered
// handler, in registry order of the phase snapshot.
type HookRunner interface {
	// Each calls invoke with each matching handler. An ordinary failure from
	// invoke is reported and the next handler runs; once the invocation is
	// signalled the failure propagates.
	Each(name string, invoke func(handler any) error) error
}

// TaskRuntime is the operation set of one task invocation. Every operation
// rejects after the invocation ends; watches acquired through it stop at
// invocation end. Generic upstream methods are erased to `any`.
type TaskRuntime interface {
	HookApi
	DocumentObserver
	DocumentReader

	TaskID() Id
	ConversationID() Id
	// Signal is cancelled when the run is signalled by abortTask, the harness
	// closes, or the invocation ends.
	Signal() context.Context
	// Registry is the phase snapshot, refreshed at every phase boundary.
	Registry() RegistrySnapshot
	// Agent resolves the conversation's agent at most once per phase.
	Agent(ctx chord.Context) (Agent, error)
	// Settings is the harness settings, resolved at each access.
	Settings() Settings
	Models() *ai.Models
	// Env builds the conversation's environment.
	Env(ctx chord.Context) (ExecutionEnv, error)
	// Hooks are the handlers of this task's name from the selected extensions,
	// in extension order.
	Hooks() HookRunner

	// Commit commits on the Session line after rereading the task. A returned
	// state replaces the task's state in the same commit.
	Commit(change func(tx *Transaction, current RunningTask) (*NextTaskState, error), ctx chord.Context) error
	// Memo reads a durable memo of this task.
	Memo(ctx chord.Context, name string) (chord.JsonValue, bool, error)
	// MemoSet stores candidate unless a memo already exists; returns the
	// durable winner.
	MemoSet(ctx chord.Context, name string, candidate chord.JsonValue) (chord.JsonValue, error)
	// GetTask reads a committed task record.
	GetTask(id Id, ctx chord.Context) (*TaskRecord, error)
	// WaitForTask resolves with the task's terminal receipt.
	WaitForTask(id Id, ctx chord.Context) (SettledTask, error)
	// Outcomes returns the outcomes of terminal tasks, in order.
	Outcomes(ids []Id, ctx chord.Context) ([]TaskOutcome, error)
	// Conversation is an invocation-bound handle of an existing conversation.
	Conversation(id Id, ctx chord.Context) (ConversationHandle, bool, error)
	// Entry reads a committed entry visible from the task's conversation.
	Entry(ctx chord.Context, id Id) (*EntryRecord, error)
	// Context derives the committed transcript and model context.
	Context(conversationID Id, ctx chord.Context, at *Id) (ContextView, error)
	// Now is the harness clock.
	Now() int64
	// Report forwards a non-fatal failure to the harness.
	Report(err error)
	// Sleep resolves once the harness clock reaches until.
	Sleep(until int64, ctx chord.Context) error
}
