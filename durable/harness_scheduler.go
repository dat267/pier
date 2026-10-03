package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
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

// EndedInvocationError reports work that used an invocation after it ended.
type EndedInvocationError struct {
	TaskID Id
}

func (e *EndedInvocationError) Error() string {
	return fmt.Sprintf("Task %d invocation has ended", e.TaskID)
}

// StoppableWatch is a watch an invocation stops when it ends.
type StoppableWatch interface {
	Stop() WatchEnd
}

// Invocation is one in-memory execution of a task in run or abort mode.
type Invocation struct {
	TaskID         Id
	ConversationID Id
	// Mode is "run" or "abort".
	Mode string

	context context.Context
	cancel  context.CancelFunc
	watches []StoppableWatch
	done    chan struct{}
	ended   atomic.Bool
}

// NewInvocation builds an invocation bound to parent, in run or abort mode.
func NewInvocation(taskID, conversationID Id, mode string, parent context.Context) *Invocation {
	if parent == nil {
		parent = context.Background()
	}
	context, cancel := context.WithCancel(parent)
	return &Invocation{
		TaskID: taskID, ConversationID: conversationID, Mode: mode,
		context: context, cancel: cancel, done: make(chan struct{}),
	}
}

// Context is the invocation's signalled context. It is cancelled when the
// invocation ends.
func (i *Invocation) Context() context.Context { return i.context }

// Done settles when the invocation ends.
func (i *Invocation) Done() <-chan struct{} { return i.done }

// Ended reports whether the invocation has ended; its operations reject from
// then on.
func (i *Invocation) Ended() bool { return i.ended.Load() }

// AddWatch registers a watch stopped at invocation end.
func (i *Invocation) AddWatch(watch StoppableWatch) {
	if i.ended.Load() {
		watch.Stop()
		return
	}
	i.watches = append(i.watches, watch)
}

// End ends the invocation: its runtime operations reject from now on, its
// signal aborts, and its watches stop.
func (i *Invocation) End() {
	if i.ended.Swap(true) {
		return
	}
	for _, watch := range i.watches {
		watch.Stop()
	}
	i.watches = nil
	i.cancel()
	close(i.done)
}

// AssertLive rejects an operation issued after the invocation ended.
func (i *Invocation) AssertLive() error {
	if i.ended.Load() {
		return &EndedInvocationError{TaskID: i.TaskID}
	}
	return nil
}

// GateTask applies the invocation gate to a reread task on the line, returning
// the running record or the error that ends the operation.
func GateTask(invocation *Invocation, found *TaskRecord, closing bool) (TaskRecord, error) {
	if err := invocation.AssertLive(); err != nil {
		return TaskRecord{}, err
	}
	if closing {
		return TaskRecord{}, ClosedError()
	}
	if found == nil {
		return TaskRecord{}, fmt.Errorf("Task %d is terminal", invocation.TaskID)
	}
	if found.State.Status != TaskRunning {
		return TaskRecord{}, fmt.Errorf("Task %d is %s", invocation.TaskID, found.State.Status)
	}
	if invocation.Mode == "run" && found.AbortRequested {
		return TaskRecord{}, fmt.Errorf("Task %d has a durable abort mark", invocation.TaskID)
	}
	return *found, nil
}

// SchedulerScanPageSize is the scheduler's scan page size.
const SchedulerScanPageSize = 256

// ReservationPlan is one task the scheduler reserves: its definition, the
// record to run, the running state to stage when it changed, and the mode.
type ReservationPlan struct {
	TaskID Id
	Task   *Task
	Record TaskRecord
	// SetRunning is the running state to write when the record changed or was
	// not already running.
	SetRunning *TaskRecord
	// Mode is "run" or "abort".
	Mode string
}

// OrphanTermination is an abort-marked task no registered definition can take;
// the scheduler settles it as orphaned.
type OrphanTermination struct {
	Record TaskRecord
	Reason string
}

// PlanReservations reserves every eligible task in one pass. A task waits for
// its live ordinary owned work when abort-marked, otherwise for the live part
// of a wait's `on`; a completing task is left to finalization. An abort-marked
// task no definition can take is orphaned.
func PlanReservations(
	records []TaskRecord,
	graph SchedulerGraph,
	registry RegistrySnapshot,
	invocations map[Id]bool,
	failed map[Id]FailedMigration,
	report func(error),
) ([]ReservationPlan, []OrphanTermination) {
	if registry == nil {
		return nil, nil
	}
	live := RecordsByID(records)
	owned := OwnedLive(records, graph)
	plans := []ReservationPlan{}
	orphans := []OrphanTermination{}
	for _, record := range records {
		if invocations[record.ID] {
			continue
		}
		if len(WaitingOn(record, owned, live)) > 0 {
			continue
		}
		if record.State.Status == TaskCompleting {
			continue
		}
		mode := "run"
		if record.AbortRequested {
			mode = "abort"
		}
		resolution := ResolveRecord(record, registry, failed, report)
		if resolution.Kind == SchedulerBlocked {
			if mode == "abort" {
				orphans = append(orphans, OrphanTermination{Record: record, Reason: resolution.Reason})
			}
			continue
		}
		plan := ReservationPlan{TaskID: record.ID, Task: resolution.Task, Record: resolution.Record, Mode: mode}
		if resolution.Migrated || record.State.Status != TaskRunning {
			running := WithState(resolution.Record, TaskState{
				Status: TaskRunning, Checkpoint: resolution.Record.State.Checkpoint,
			})
			plan.SetRunning = &running
		}
		plans = append(plans, plan)
	}
	return plans, orphans
}

// LoadLiveTasks loads every live task into the mirror and changes surviving
// running tasks back to pending. Every table read happens before the first
// write.
func LoadLiveTasks(tx *Transaction, mirror *SchedulerMirror) error {
	records := []TaskRecord{}
	for _, status := range liveTaskStatuses {
		statusValue := status
		page, err := ScanAll(func(cursor Cursor) (Page[TaskRecord], error) {
			return tx.ScanTasks(TaskQuery{Status: &statusValue}, SchedulerScanPageSize, cursor)
		})
		if err != nil {
			return err
		}
		records = append(records, page...)
	}
	for _, record := range records {
		mirror.Live[record.ID] = record
		if record.State.Status == TaskRunning {
			pending := WithState(record, TaskState{Status: TaskPending, Checkpoint: record.State.Checkpoint})
			if err := tx.SetTask(&pending); err != nil {
				return err
			}
		}
		if record.State.Status == TaskWaiting && record.State.Policy == JoinFailFast {
			mirror.FailFastChecks[record.ID] = true
		}
	}
	return nil
}

// SchedulerMirror mirrors every committed non-terminal task record and the
// ownership fields a walk passes through.
type SchedulerMirror struct {
	// Live is the committed non-terminal records.
	Live map[Id]TaskRecord
	// Settled holds the ownership fields of terminal tasks that own a loaded
	// conversation.
	Settled map[Id]SchedulerTaskNode
	// Edges maps a loaded conversation to its owner task; a present nil means
	// ownerless.
	Edges map[Id]*Id
	// ConversationOwners marks tasks that own a loaded conversation.
	ConversationOwners map[Id]bool
	// FailedMigrations remembers migrations already tried.
	FailedMigrations map[Id]FailedMigration
	// FailFastChecks are the failFast waiters the next reconcile checks.
	FailFastChecks map[Id]bool
}

// NewSchedulerMirror builds an empty mirror.
func NewSchedulerMirror() *SchedulerMirror {
	return &SchedulerMirror{
		Live: map[Id]TaskRecord{}, Settled: map[Id]SchedulerTaskNode{}, Edges: map[Id]*Id{},
		ConversationOwners: map[Id]bool{}, FailedMigrations: map[Id]FailedMigration{}, FailFastChecks: map[Id]bool{},
	}
}

// Graph is the ownership graph of the mirror.
func (m *SchedulerMirror) Graph() SchedulerGraph {
	settled := make(map[Id]TaskRecord, len(m.Settled))
	for id, node := range m.Settled {
		owner := node.Owner
		settled[id] = TaskRecord{ID: id, ConversationID: node.ConversationID, Owner: owner, Background: node.Background}
	}
	return SchedulerGraph{Live: m.Live, Settled: settled, Edges: m.Edges}
}

// MirrorEffects are the side effects of applying one publication.
type MirrorEffects struct {
	// Terminal are the records that became terminal; the caller resolves the
	// task waiters for them.
	Terminal []Id
	// SignalInvocations are the newly abort-marked tasks whose run invocation
	// is signalled.
	SignalInvocations []Id
	// NewEdges are the conversations whose owner edge was learned.
	NewEdges map[Id]*Id
	// Updated are the non-terminal records in publication order.
	Updated []TaskRecord

	CascadePending    bool
	ScheduleReconcile bool
}

// Observe applies one publication to the mirror.
func (m *SchedulerMirror) Observe(publication CommitPublication) MirrorEffects {
	effects := MirrorEffects{NewEdges: map[Id]*Id{}}
	updated := []TaskRecord{}
	failed := []Id{}
	for _, change := range publication.Changes {
		if change.Write == nil || change.Write.Type != "task" || change.Write.Task == nil {
			continue
		}
		record := *change.Write.Task
		previous, had := m.Live[record.ID]
		if FailedOutcome(record) && (!had || !FailedOutcome(previous)) {
			failed = append(failed, record.ID)
		}
		if record.State.Status == TaskTerminal {
			delete(m.Live, record.ID)
			delete(m.FailedMigrations, record.ID)
			delete(m.FailFastChecks, record.ID)
			if m.ConversationOwners[record.ID] {
				m.Settled[record.ID] = NodeOf(record)
			}
			effects.Terminal = append(effects.Terminal, record.ID)
			effects.ScheduleReconcile = true
			continue
		}
		if record.AbortRequested && !(had && previous.AbortRequested) {
			effects.CascadePending = true
			// Signal a run invocation of the newly marked task; its next step
			// ends it.
			effects.SignalInvocations = append(effects.SignalInvocations, record.ID)
		}
		status := record.State.Status
		if status == TaskCompleting && !(had && previous.State.Status == TaskCompleting) {
			if CancellationIntent(record) {
				effects.CascadePending = true
			}
			effects.ScheduleReconcile = true
		}
		if status == TaskWaiting && record.State.Policy == JoinFailFast &&
			!(had && previous.State.Status == TaskWaiting) {
			m.FailFastChecks[record.ID] = true
			effects.ScheduleReconcile = true
		}
		m.Live[record.ID] = record
		updated = append(updated, record)
	}
	for _, id := range failed {
		for _, record := range m.Live {
			state := record.State
			if state.Status == TaskWaiting && state.Policy == JoinFailFast && idListContains(state.On, id) {
				m.FailFastChecks[record.ID] = true
				effects.ScheduleReconcile = true
			}
		}
	}
	for _, change := range publication.Changes {
		if change.Write == nil || change.Write.Type != "conversation" || change.Write.Conversation == nil {
			continue
		}
		conversation := change.Write.Conversation
		if _, present := m.Edges[conversation.ID]; present {
			continue
		}
		var owner *Id
		if conversation.Owner != nil {
			taskID := conversation.Owner.TaskID
			owner = &taskID
		}
		m.Edges[conversation.ID] = owner
		effects.NewEdges[conversation.ID] = owner
	}
	graph := m.Graph()
	for _, change := range publication.Changes {
		// A queued input below a cancelled owner is withdrawn, even after its
		// cascade.
		if change.Write == nil || change.Write.Type != "submission" || change.Write.Submission == nil {
			continue
		}
		submission := change.Write.Submission
		if submission.Status != SubmissionQueued || submission.Type != SubmissionTypeInput {
			continue
		}
		conversation := submission.ConversationID
		if !ChainKnown(SchedulerUp{Conversation: &conversation}, graph) ||
			BelowCancelled(SchedulerUp{Conversation: &conversation}, graph) {
			effects.CascadePending = true
		}
	}
	for _, record := range updated {
		// Work created below a cancelled owner, even after its cascade, is
		// aborted too.
		if !ChainKnown(ParentOf(NodeOf(record)), graph) {
			effects.ScheduleReconcile = true
		} else if !record.Background && !record.AbortRequested && BelowCancelled(ParentOf(NodeOf(record)), graph) {
			effects.CascadePending = true
		}
	}
	effects.Updated = updated
	return effects
}

func idListContains(ids []Id, target Id) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

// ReconcileTasks runs one reconcile pass: derive cancellation marks from
// intentful owners and failFast checks, withdraw the inputs of cancelled
// conversations, and finalize every completing task that is free. It returns
// the first failure, which the caller reports and retries with the next commit
// since any pass may have staged marks.
func ReconcileTasks(
	tx *Transaction,
	live []TaskRecord,
	graph SchedulerGraph,
	queuedConversations []Id,
	failFastChecks []Id,
	load func(id Id) (*TaskRecord, error),
	withdrawInputs func(tx *Transaction, conversationID Id) error,
	settleOutcome func(tx *Transaction, record TaskRecord, outcome *TaskOutcome) error,
) error {
	liveByID := RecordsByID(live)
	marked := map[Id]bool{}
	mark := func(record TaskRecord) error {
		if record.AbortRequested || marked[record.ID] {
			return nil
		}
		marked[record.ID] = true
		replacement := record
		replacement.AbortRequested = true
		return tx.SetTask(&replacement)
	}
	// Loading edges can reveal a cancelled owner, so marks are derived on
	// every pass.
	for _, record := range live {
		if record.Background {
			continue
		}
		if BelowCancelled(ParentOf(NodeOf(record)), graph) {
			if err := mark(record); err != nil {
				return err
			}
		}
	}
	for _, id := range failFastChecks {
		waiter, present := liveByID[id]
		if !present || waiter.State.Status != TaskWaiting {
			continue
		}
		failed, err := AnyFailed(waiter.State.On, liveByID, load)
		if err != nil {
			return err
		}
		if !failed {
			continue
		}
		// Every other live task: the failed one keeps its own outcome.
		for _, member := range waiter.State.On {
			if record, present := liveByID[member]; present && !FailedOutcome(record) {
				if err := mark(record); err != nil {
					return err
				}
			}
		}
	}
	for _, id := range queuedConversations {
		conversation := id
		if BelowCancelled(SchedulerUp{Conversation: &conversation}, graph) {
			if err := withdrawInputs(tx, id); err != nil {
				return err
			}
		}
	}
	return FinalizeCompleting(tx, live, settleOutcome)
}

// DeriveCancellationMarks is the ids a reconcile pass marks: every live
// non-background task whose owner chain carries cancellation intent, skipping
// tasks already marked.
func DeriveCancellationMarks(live []TaskRecord, graph SchedulerGraph) []Id {
	marks := []Id{}
	seen := map[Id]bool{}
	for _, record := range live {
		if record.Background || record.AbortRequested || seen[record.ID] {
			continue
		}
		if BelowCancelled(ParentOf(NodeOf(record)), graph) {
			seen[record.ID] = true
			marks = append(marks, record.ID)
		}
	}
	return marks
}

// AnyFailed reports whether any of ids holds or ended with an outcome other
// than completed, falling back to load for tasks not in the live view.
func AnyFailed(ids []Id, live map[Id]TaskRecord, load func(id Id) (*TaskRecord, error)) (bool, error) {
	for _, id := range ids {
		record, present := live[id]
		if !present {
			loaded, err := load(id)
			if err != nil {
				return false, err
			}
			if loaded == nil {
				continue
			}
			record = *loaded
		}
		if FailedOutcome(record) {
			return true, nil
		}
	}
	return false, nil
}

// FailFastMarks is the ids a failFast check marks once a wait's `on` has a
// failed member: every other live member, so the failed one keeps its own
// outcome.
func FailFastMarks(waiter TaskRecord, live map[Id]TaskRecord, anyFailed func(ids []Id) (bool, error)) ([]Id, error) {
	if waiter.State.Status != TaskWaiting {
		return nil, nil
	}
	failed, err := anyFailed(waiter.State.On)
	if err != nil || !failed {
		return nil, err
	}
	marks := []Id{}
	for _, id := range waiter.State.On {
		record, present := live[id]
		if present && !FailedOutcome(record) {
			marks = append(marks, id)
		}
	}
	return marks, nil
}

// CancelledScopes is the conversations (with queued submissions) whose owner
// chain carries cancellation intent, and whose inputs a reconcile pass
// withdraws.
func CancelledScopes(conversations []Id, graph SchedulerGraph) []Id {
	cancelled := []Id{}
	for _, id := range conversations {
		conversation := id
		if BelowCancelled(SchedulerUp{Conversation: &conversation}, graph) {
			cancelled = append(cancelled, id)
		}
	}
	return cancelled
}

// MemoSet stores candidate unless a memo already exists; it returns the
// durable winner. A memo is owned by the task record it is written into.
func MemoSet(tx *Transaction, current TaskRecord, name string, candidate json.RawMessage) (json.RawMessage, error) {
	if winner, present := MemoOf(&current, name); present {
		return winner, nil
	}
	memos := make(map[string]json.RawMessage, len(current.Memos)+1)
	for key, value := range current.Memos {
		memos[key] = value
	}
	memos[name] = candidate
	replacement := current
	replacement.Memos = memos
	if err := tx.SetTask(&replacement); err != nil {
		return nil, err
	}
	return candidate, nil
}

// FinalizeCompleting writes the terminal record of every completing task
// without live ordinary owned work. Finalizing one can free its owner, so it
// repeats over the commit's candidates until nothing changes; a faulted or
// orphaned outcome gets its harness cleanup here.
func FinalizeCompleting(
	tx *Transaction,
	live []TaskRecord,
	settleOutcome func(tx *Transaction, record TaskRecord, outcome *TaskOutcome) error,
) error {
	for {
		overlay := OverlayOf(tx)
		graph := SchedulerGraph{Live: RecordsByID(live), Overlay: &overlay}
		records := LiveRecordsWithOverlay(live, overlay)
		owned := OwnedTaskIDs(records, graph)
		done := []TaskRecord{}
		for _, record := range records {
			if record.State.Status == TaskCompleting && !owned[record.ID] {
				done = append(done, record)
			}
		}
		if len(done) == 0 {
			return nil
		}
		for _, record := range done {
			outcome := record.State.Outcome
			terminal := WithState(record, TaskState{Status: TaskTerminal, Outcome: outcome})
			if err := tx.SetTask(&terminal); err != nil {
				return err
			}
			// Only the scheduler writes faulted and orphaned; their cleanup
			// waits for this commit.
			if outcome != nil && (outcome.Status == OutcomeFaulted || outcome.Status == OutcomeOrphaned) && settleOutcome != nil {
				if err := settleOutcome(tx, record, outcome); err != nil {
					return err
				}
			}
		}
	}
}

// RecordsByID indexes records by id.
func RecordsByID(records []TaskRecord) map[Id]TaskRecord {
	byID := make(map[Id]TaskRecord, len(records))
	for _, record := range records {
		byID[record.ID] = record
	}
	return byID
}

// LiveRecordsWithOverlay returns the live records with the overlay's
// candidates replacing committed ones; terminal candidates are dropped.
func LiveRecordsWithOverlay(live []TaskRecord, overlay SchedulerOverlay) []TaskRecord {
	result := []TaskRecord{}
	seen := map[Id]bool{}
	for _, record := range live {
		candidate, present := overlay.Tasks[record.ID]
		if present {
			record = candidate
		}
		seen[record.ID] = true
		if record.State.Status != TaskTerminal {
			result = append(result, record)
		}
	}
	for _, record := range overlay.Tasks {
		if seen[record.ID] || record.State.Status == TaskTerminal {
			continue
		}
		result = append(result, record)
	}
	return result
}

// OwnedTaskIDs is the set of owner tasks with live non-background work below
// them, up to and including the first background owner.
func OwnedTaskIDs(records []TaskRecord, graph SchedulerGraph) map[Id]bool {
	owned := map[Id]bool{}
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
			owned[*step.Task] = true
			if step.Node.Background {
				break
			}
		}
	}
	return owned
}

// CommitTaskState replaces a running task's state with what it committed. A
// terminal state holds as completing while ordinary owned work is live, judged
// on the commit's candidates, so work the same commit creates below the task
// counts. A wait is validated first.
func CommitTaskState(
	tx *Transaction,
	live []TaskRecord,
	current TaskRecord,
	next NextTaskState,
	validateWait func(on []Id, policy string) error,
) error {
	if next.Status == TaskWaiting && validateWait != nil {
		if err := validateWait(next.On, next.Policy); err != nil {
			return err
		}
	}
	overlay := OverlayOf(tx)
	graph := SchedulerGraph{Live: RecordsByID(live), Overlay: &overlay}
	if next.Status == TaskTerminal {
		records := LiveRecordsWithOverlay(live, overlay)
		if OwnedTaskIDs(records, graph)[current.ID] {
			completing := WithState(current, TaskState{Status: TaskCompleting, Outcome: next.Outcome})
			return tx.SetTask(&completing)
		}
	}
	replacement := WithState(current, TaskState{
		Status: next.Status, Checkpoint: next.Checkpoint, On: next.On, Policy: next.Policy, Outcome: next.Outcome,
	})
	return tx.SetTask(&replacement)
}

// TerminateTask writes an outcome the scheduler decided. While the task's
// ordinary owned work is live it holds as completing and its harness cleanup
// waits for the final commit; otherwise it is terminal with its cleanup.
func TerminateTask(
	tx *Transaction,
	live []TaskRecord,
	record TaskRecord,
	outcome *TaskOutcome,
	settleOutcome func(tx *Transaction, record TaskRecord, outcome *TaskOutcome) error,
) error {
	overlay := OverlayOf(tx)
	graph := SchedulerGraph{Live: RecordsByID(live), Overlay: &overlay}
	records := LiveRecordsWithOverlay(live, overlay)
	if OwnedTaskIDs(records, graph)[record.ID] {
		completing := WithState(record, TaskState{Status: TaskCompleting, Outcome: outcome})
		return tx.SetTask(&completing)
	}
	terminal := WithState(record, TaskState{Status: TaskTerminal, Outcome: outcome})
	if err := tx.SetTask(&terminal); err != nil {
		return err
	}
	if settleOutcome != nil {
		return settleOutcome(tx, record, outcome)
	}
	return nil
}

// PhaseResult is the outcome of the phase that just returned.
type PhaseResult struct {
	Checkpoint json.RawMessage
	// HasFailure marks a phase that failed; Failure is its error (which may
	// itself be nil).
	HasFailure bool
	Failure    error
}

// PhaseState is what a phase decision refreshes: the definition in use, the
// registry snapshot, and the replacement definition already reported.
type PhaseState struct {
	Task     *Task
	Snapshot RegistrySnapshot
	Refresh  func() RegistrySnapshot
	Reported *Task
}

// PhaseDecision is a step decision: continue with the next phase, end the
// invocation, or end it by writing `faulted`.
type PhaseDecision struct {
	Continue bool
	Fault    error
}

// CheckpointPhase reads a checkpoint's phase.
func CheckpointPhase(checkpoint json.RawMessage) string {
	var probe struct {
		Phase string `json:"phase"`
	}
	if err := json.Unmarshal(checkpoint, &probe); err != nil {
		return ""
	}
	return probe.Phase
}

// DefinitionMismatchError reports a task that keeps running under its old
// definition because the registry resolved no replacement that can take it.
type DefinitionMismatchError struct {
	TaskID Id
	Kind   string
	Cause  string
}

func (e *DefinitionMismatchError) Error() string {
	return fmt.Sprintf("Task %d keeps running under its old %s definition", e.TaskID, e.Kind)
}

// DecidePhase applies the precedence rules for a run invocation, on the line.
// It returns whether the invocation continues with the next phase.
func DecidePhase(tx *Transaction, current TaskRecord, previous *PhaseResult, state *PhaseState, report func(error)) (PhaseDecision, error) {
	// Abort mark: end; a fresh abort invocation starts once the task's ordinary
	// owned work is gone.
	if current.AbortRequested {
		return PhaseDecision{}, nil
	}
	if previous == nil {
		return PhaseDecision{Continue: true}, nil
	}
	if previous.HasFailure {
		return PhaseDecision{Fault: previous.Failure}, nil
	}
	// No durable progress.
	if JSONEqual(decodeJSONValue(current.State.Checkpoint), decodeJSONValue(previous.Checkpoint)) {
		return PhaseDecision{Fault: fmt.Errorf(
			"Task %s phase %s returned without durable progress", current.Kind, CheckpointPhase(previous.Checkpoint),
		)}, nil
	}
	// Progress: refresh the snapshot; hand over to a replacement definition
	// that can take the task.
	if state.Refresh != nil {
		state.Snapshot = state.Refresh()
	}
	if state.Snapshot == nil {
		return PhaseDecision{Continue: true}, nil
	}
	next := state.Snapshot.Task(current.Kind)
	if next != state.Task {
		if next != nil && CanReserve(*next, current) {
			pending := WithState(current, TaskState{Status: TaskPending, Checkpoint: current.State.Checkpoint})
			if err := tx.SetTask(&pending); err != nil {
				return PhaseDecision{}, err
			}
			return PhaseDecision{}, nil
		}
		if state.Reported == nil || state.Reported != next {
			state.Reported = next
			cause := "missing_task"
			if next != nil {
				cause = "incompatible_task"
			}
			if report != nil {
				report(&DefinitionMismatchError{TaskID: current.ID, Kind: current.Kind, Cause: cause})
			}
		}
	}
	return PhaseDecision{Continue: true}, nil
}

func decodeJSONValue(raw json.RawMessage) chord.JsonValue {
	if len(raw) == 0 {
		return nil
	}
	var value chord.JsonValue
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}

// ValidateWait checks that a wait names existing tasks other than the waiter
// and its owners; failFast only tasks the waiter owns. An abort handler cannot
// wait.
func ValidateWait(mode string, current TaskRecord, on []Id, policy string, owners map[Id]bool, member func(id Id) (*TaskRecord, error)) error {
	if mode == "abort" {
		return fmt.Errorf("Abort handler of task %d cannot wait", current.ID)
	}
	for _, id := range on {
		if id == current.ID || owners[id] {
			return fmt.Errorf("Task %d cannot wait on itself or its owner %d", current.ID, id)
		}
		record, err := member(id)
		if err != nil {
			return err
		}
		if record == nil {
			return fmt.Errorf("Task %d does not exist", id)
		}
		if policy == JoinFailFast && (record.Owner == nil || *record.Owner != current.ID) {
			return fmt.Errorf("Task %d can wait failFast only on tasks it owns; %d is not one", current.ID, id)
		}
	}
	return nil
}

// Idle reports whether a scope has no live non-background task; a task whose
// owner edges are not loaded yet counts as inside.
func Idle(conversationID *Id, records []TaskRecord, graph SchedulerGraph) bool {
	scope := SchedulerScope{Conversation: conversationID}
	if conversationID == nil {
		scope = SchedulerScope{Roots: true}
	}
	for _, record := range records {
		if record.Background {
			continue
		}
		inScope, known := InScope(ParentOf(NodeOf(record)), scope, false, graph)
		if !known || inScope {
			return false
		}
	}
	return true
}

// FailedMigration records a definition whose migration could not take a task;
// it is retried only once the registry resolves another definition.
type FailedMigration struct {
	TaskName string
	Version  int
	Error    error
}

// SchedulerFit is a definition that can take a record, or why none can.
type SchedulerFit struct {
	Task     *Task
	Migrates bool
	Reason   string
	Error    error
}

// FitRecord decides whether a definition can take the record, deciding it
// without running task code.
func FitRecord(record TaskRecord, task *Task, failed map[Id]FailedMigration) SchedulerFit {
	if task == nil {
		return SchedulerFit{Reason: BlockedMissingTask}
	}
	version := task.Definition.Version
	if version == record.Version {
		return SchedulerFit{Task: task}
	}
	if version < record.Version {
		return SchedulerFit{Reason: BlockedTaskTooOld}
	}
	if failure, present := failed[record.ID]; present &&
		failure.TaskName == task.Definition.Name && failure.Version == task.Definition.Version {
		return SchedulerFit{Task: task, Reason: BlockedMigrationFailed, Error: failure.Error}
	}
	return SchedulerFit{Task: task, Migrates: true}
}

// Scheduler resolution kinds.
const (
	SchedulerReady   = "ready"
	SchedulerBlocked = "blocked"
)

// SchedulerResolution is a definition that can take a record, or why none can.
type SchedulerResolution struct {
	Kind   string
	Task   *Task
	Record TaskRecord
	Reason string
	// Migrated reports that the record was converted to the definition's
	// version.
	Migrated bool
}

// ResolveRecord resolves the record's definition by kind, migrating an older
// stored version.
func ResolveRecord(record TaskRecord, snapshot RegistrySnapshot, failed map[Id]FailedMigration, report func(error)) SchedulerResolution {
	fit := FitRecord(record, snapshot.Task(record.Kind), failed)
	if fit.Reason != "" {
		return SchedulerResolution{Kind: SchedulerBlocked, Reason: fit.Reason}
	}
	if !fit.Migrates {
		return SchedulerResolution{Kind: SchedulerReady, Task: fit.Task, Record: record}
	}
	definition := fit.Task.Definition
	if definition.Migrate == nil {
		return SchedulerResolution{Kind: SchedulerBlocked, Reason: BlockedMigrationFailed}
	}
	input, checkpoint, err := definition.Migrate(record.Input, record.State.Checkpoint, record.Version)
	if err != nil {
		failed[record.ID] = FailedMigration{TaskName: definition.Name, Version: definition.Version, Error: err}
		if report != nil {
			report(err)
		}
		return SchedulerResolution{Kind: SchedulerBlocked, Reason: BlockedMigrationFailed}
	}
	migrated := record
	migrated.Version = definition.Version
	migrated.Input = copyRawJSON(input)
	migrated.State = record.State
	migrated.State.Checkpoint = copyRawJSON(checkpoint)
	return SchedulerResolution{Kind: SchedulerReady, Task: fit.Task, Record: migrated}
}

func copyRawJSON(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	return append(json.RawMessage{}, value...)
}

// InspectTask is the derived scheduling state of one live task; it runs no
// task code.
func InspectTask(
	record TaskRecord,
	snapshot RegistrySnapshot,
	owned map[Id][]Id,
	live map[Id]TaskRecord,
	running map[Id]bool,
	failed map[Id]FailedMigration,
) TaskInspection {
	if running[record.ID] {
		return TaskInspection{Record: record, Kind: TaskInspectionRunning}
	}
	if record.State.Status == TaskCompleting {
		return TaskInspection{Record: record, Kind: TaskInspectionCompleting}
	}
	if on := WaitingOn(record, owned, live); len(on) > 0 {
		return TaskInspection{Record: record, Kind: TaskInspectionWaiting, On: on}
	}
	fit := FitRecord(record, snapshot.Task(record.Kind), failed)
	if fit.Reason != "" {
		return TaskInspection{Record: record, Kind: TaskInspectionBlocked, Reason: fit.Reason, Error: fit.Error}
	}
	if fit.Migrates && fit.Task.Definition.Migrate == nil {
		return TaskInspection{
			Record: record, Kind: TaskInspectionBlocked, Reason: BlockedMigrationFailed,
			Error: MissingMigration(record, fit.Task.Definition),
		}
	}
	return TaskInspection{Record: record, Kind: TaskInspectionReady, Migrates: fit.Migrates}
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
