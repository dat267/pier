package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dat267/pier/chord"
)

// Port of the pure task-state, ownership and JSON helpers of
// harness/scheduler.ts. The scheduler class itself lands on top of these.

// MaxTimerDelay is the longest delay a timer supports; longer sleeps wait in
// several steps.
const MaxTimerDelay = 2_147_483_647

// Blocked reasons: why a pending task cannot be reserved under a registry
// snapshot. Derived, never persisted.
const (
	BlockedMissingTask     = "missing_task"
	BlockedTaskTooOld      = "task_too_old"
	BlockedMigrationFailed = "migration_failed"
)

// IsLiveTaskStatus reports whether a status is one the scheduler mirrors.
func IsLiveTaskStatus(status string) bool {
	for _, live := range liveTaskStatuses {
		if live == status {
			return true
		}
	}
	return false
}

// SchedulerTaskNode is the immutable ownership view of a task.
type SchedulerTaskNode struct {
	ConversationID Id
	Owner          *Id
	Background     bool
}

// SchedulerUp is where a walk up the ownership tree continues.
type SchedulerUp struct {
	Task         *Id
	Conversation *Id
}

// ParentOf is the next step up from a task node.
func ParentOf(node SchedulerTaskNode) SchedulerUp {
	if node.Owner != nil {
		owner := *node.Owner
		return SchedulerUp{Task: &owner}
	}
	conversation := node.ConversationID
	return SchedulerUp{Conversation: &conversation}
}

// NodeOf is the immutable ownership view of a record.
func NodeOf(record TaskRecord) SchedulerTaskNode {
	return SchedulerTaskNode{
		ConversationID: record.ConversationID, Owner: record.Owner, Background: record.Background,
	}
}

// SchedulerOverlay is the task and conversation-owner overlay of a
// transaction's staged records.
type SchedulerOverlay struct {
	Tasks map[Id]TaskRecord
	// Edges maps a conversation to its owner task, or nil for ownerless ones.
	Edges map[Id]*Id
}

// OverlayOf reads a transaction's staged tasks and conversations.
func OverlayOf(tx *Transaction) SchedulerOverlay {
	overlay := SchedulerOverlay{Tasks: map[Id]TaskRecord{}, Edges: map[Id]*Id{}}
	for _, record := range tx.StagedTasks() {
		overlay.Tasks[record.ID] = *record
	}
	for _, record := range tx.StagedConversations() {
		var owner *Id
		if record.Owner != nil {
			taskID := record.Owner.TaskID
			owner = &taskID
		}
		overlay.Edges[record.ID] = owner
	}
	return overlay
}

// CancellationIntent reports whether a record carries cancellation intent:
// an abort mark or a failed outcome, while not terminal.
func CancellationIntent(record TaskRecord) bool {
	return record.State.Status != TaskTerminal && (record.AbortRequested || FailedOutcome(record))
}

// FailedOutcome reports whether the record holds or ends with an outcome other
// than completed.
func FailedOutcome(record TaskRecord) bool {
	status := record.State.Status
	if status != TaskCompleting && status != TaskTerminal {
		return false
	}
	return record.State.Outcome != nil && record.State.Outcome.Status != OutcomeCompleted
}

// MemoOf reads a task's own memo entry only; memo names such as `toString`
// must not resolve to inherited properties.
func MemoOf(record *TaskRecord, name string) (json.RawMessage, bool) {
	if record == nil || record.Memos == nil {
		return nil, false
	}
	value, present := record.Memos[name]
	return value, present
}

// CanReserve reports whether a definition can take the task at reservation:
// the same version, or a newer one with a migration.
func CanReserve(task Task, record TaskRecord) bool {
	definition := task.Definition
	return definition.Version == record.Version ||
		(definition.Version > record.Version && definition.Migrate != nil)
}

// MissingMigration is the failure of a task with no migration to the
// definition's version.
func MissingMigration(record TaskRecord, definition TaskDefinition) error {
	return fmt.Errorf("Task %s version %d has no migration from %d", record.Kind, definition.Version, record.Version)
}

// WithState replaces a live record's state; memos disappear once an outcome is
// decided.
func WithState(record TaskRecord, state TaskState) TaskRecord {
	record.State = state
	if state.Status == TaskTerminal || state.Status == TaskCompleting {
		record.Memos = nil
	}
	return record
}

// Delay resolves once ms elapse, rejecting when ctx is cancelled.
func Delay(ms int64, ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for ms > 0 {
		step := ms
		if step > MaxTimerDelay {
			step = MaxTimerDelay
		}
		timer := time.NewTimer(time.Duration(step) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		ms -= step
	}
	return nil
}

// JSONEqual is structural equality of two JSON values; object key order is
// ignored.
func JSONEqual(left, right chord.JsonValue) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	switch typed := left.(type) {
	case map[string]any:
		other, ok := right.(map[string]any)
		if !ok || len(typed) != len(other) {
			return false
		}
		for key, value := range typed {
			candidate, present := other[key]
			if !present || !JSONEqual(value, candidate) {
				return false
			}
		}
		return true
	case []any:
		other, ok := right.([]any)
		if !ok || len(typed) != len(other) {
			return false
		}
		for index := range typed {
			if !JSONEqual(typed[index], other[index]) {
				return false
			}
		}
		return true
	default:
		return left == right
	}
}
