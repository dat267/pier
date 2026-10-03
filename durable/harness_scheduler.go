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

// SchedulerScope is the traversal scope of an idle wait: one conversation, or
// every ownerless root.
type SchedulerScope struct {
	Conversation *Id
	Roots        bool
}

// SchedulerStep is one step of a walk up the ownership tree.
type SchedulerStep struct {
	Unknown      bool
	Task         *Id
	Node         *SchedulerTaskNode
	Conversation *Id
}

// SchedulerGraph is the loaded task/conversation graph a walk reads.
type SchedulerGraph struct {
	Live    map[Id]TaskRecord
	Settled map[Id]TaskRecord
	// Edges maps a loaded conversation to its owner task; a present nil means
	// ownerless.
	Edges map[Id]*Id
	// Overlay, when set, replaces committed records and edges.
	Overlay *SchedulerOverlay
}

// Node is the graph's view of a task node: the overlay, then live, then
// settled.
func (g SchedulerGraph) Node(id Id) (SchedulerTaskNode, bool) {
	if g.Overlay != nil {
		if record, present := g.Overlay.Tasks[id]; present {
			return NodeOf(record), true
		}
	}
	if record, present := g.Live[id]; present {
		return NodeOf(record), true
	}
	if record, present := g.Settled[id]; present {
		return NodeOf(record), true
	}
	return SchedulerTaskNode{}, false
}

// LiveOf is the graph's live view of a task record.
func (g SchedulerGraph) LiveOf(id Id) (TaskRecord, bool) {
	if g.Overlay != nil {
		if record, present := g.Overlay.Tasks[id]; present {
			return record, true
		}
	}
	record, present := g.Live[id]
	return record, present
}

// Edge is the owner task of a conversation; present nil means ownerless.
func (g SchedulerGraph) Edge(id Id) (*Id, bool) {
	if g.Overlay != nil {
		if owner, present := g.Overlay.Edges[id]; present {
			return owner, true
		}
	}
	owner, present := g.Edges[id]
	return owner, present
}

// Above walks up from start: owner tasks and conversations, ending at an
// ownerless root or an edge not loaded yet.
func Above(start SchedulerUp, graph SchedulerGraph) []SchedulerStep {
	steps := []SchedulerStep{}
	at := start
	present := true
	for present {
		if at.Task != nil {
			node, ok := graph.Node(*at.Task)
			if !ok {
				return append(steps, SchedulerStep{Unknown: true})
			}
			taskID := *at.Task
			nodeCopy := node
			steps = append(steps, SchedulerStep{Task: &taskID, Node: &nodeCopy})
			at = ParentOf(node)
			continue
		}
		conversation := *at.Conversation
		steps = append(steps, SchedulerStep{Conversation: &conversation})
		edge, ok := graph.Edge(*at.Conversation)
		if !ok {
			return append(steps, SchedulerStep{Unknown: true})
		}
		if edge == nil {
			present = false
			continue
		}
		at = SchedulerUp{Task: edge}
	}
	return steps
}

// ChainKnown reports whether every owner above start is loaded.
func ChainKnown(start SchedulerUp, graph SchedulerGraph) bool {
	for _, step := range Above(start, graph) {
		if step.Unknown {
			return false
		}
	}
	return true
}

// OwnedLive maps every owner task to the live non-background tasks below it,
// up to and including the first background owner. Records must be supplied in
// the scheduler's live order.
func OwnedLive(records []TaskRecord, graph SchedulerGraph) map[Id][]Id {
	owned := map[Id][]Id{}
	for _, record := range records {
		if record.Background {
			continue
		}
		for _, step := range Above(ParentOf(NodeOf(record)), graph) {
			if step.Unknown {
				break
			}
			if step.Task == nil {
				continue
			}
			owned[*step.Task] = append(owned[*step.Task], record.ID)
			if step.Node.Background {
				break
			}
		}
	}
	return owned
}

// InScope reports whether ordinary traversal from start reaches scope: walking
// up reaches the scope's conversation, or an ownerless one for roots, without
// crossing a background owner unless crossBackground. The second result is
// false while an edge is not loaded.
func InScope(start SchedulerUp, scope SchedulerScope, crossBackground bool, graph SchedulerGraph) (bool, bool) {
	for _, step := range Above(start, graph) {
		if step.Unknown {
			return false, false
		}
		if step.Conversation != nil {
			if scope.Conversation != nil && *step.Conversation == *scope.Conversation {
				return true, true
			}
		} else if step.Node.Background && !crossBackground {
			return false, true
		}
	}
	return scope.Roots, true
}

// BelowCancelled reports whether a live owner's cancellation intent reaches
// start: walking up finds an owner with intent before a background owner
// without it. Terminal owners never cascade.
func BelowCancelled(start SchedulerUp, graph SchedulerGraph) bool {
	for _, step := range Above(start, graph) {
		if step.Unknown {
			return false
		}
		if step.Task == nil {
			continue
		}
		if live, present := graph.LiveOf(*step.Task); present && CancellationIntent(live) {
			return true
		}
		if step.Node.Background {
			return false
		}
	}
	return false
}

// WaitingOn is the live tasks a task waits for before its next invocation: its
// live ordinary owned work when abort-marked, otherwise the live part of a
// wait's `on`.
func WaitingOn(record TaskRecord, owned map[Id][]Id, live map[Id]TaskRecord) []Id {
	if record.AbortRequested {
		return owned[record.ID]
	}
	if record.State.Status != TaskWaiting {
		return nil
	}
	on := []Id{}
	for _, id := range record.State.On {
		if _, present := live[id]; present {
			on = append(on, id)
		}
	}
	return on
}

// Task inspection kinds (upstream TaskInspection).
const (
	TaskInspectionRunning    = "running"
	TaskInspectionReady      = "ready"
	TaskInspectionWaiting    = "waiting"
	TaskInspectionCompleting = "completing"
	TaskInspectionBlocked    = "blocked"
)

// TaskInspection is a live task and what the scheduler would do with it.
type TaskInspection struct {
	Record   TaskRecord
	Kind     string
	Migrates bool
	On       []Id
	Reason   string
	Error    error
}

// HarnessInspection is a point-in-time view of live work.
type HarnessInspection struct {
	// Scheduling is "paused", "running" or "closing".
	Scheduling string
	Tasks      []TaskInspection
	// Submissions are the queued and placed submissions, in id order.
	Submissions []SubmissionRecord
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
