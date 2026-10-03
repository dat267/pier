package durable

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/dat267/pier/chord"
)

// Port of the TaskScheduler class of harness/scheduler.ts: the durable task
// scheduler of one harness. This file holds the live mirror, the waiters and
// the open/seal/inspect surface; the drain loop and invocation runtime are
// ported on top of these.

// TaskSchedulerOptions configures a TaskScheduler.
type TaskSchedulerOptions struct {
	Session  *Session
	Storage  Storage
	Registry RegistryReader
	Now      func() int64
	Report   func(error)
	// SettleOutcome is the harness cleanup staged in the commit that makes an
	// outcome the scheduler wrote itself terminal.
	SettleOutcome func(tx *Transaction, record TaskRecord, outcome *TaskOutcome) error
	// WithdrawInputs withdraws a conversation's queued inputs, for conversation
	// abort and abort cascades.
	WithdrawInputs func(tx *Transaction, conversationID Id) error
	Context        chord.Context
}

// TaskScheduler is the durable task scheduler of one harness.
type TaskScheduler struct {
	session  *Session
	storage  Storage
	registry RegistryReader
	now      func() int64
	report   func(error)
	settle   func(tx *Transaction, record TaskRecord, outcome *TaskOutcome) error
	withdraw func(tx *Transaction, conversationID Id) error
	context  chord.Context

	unsubscribeRegistry func()

	mu             sync.Mutex
	mirror         *SchedulerMirror
	invocations    map[Id]*Invocation
	taskWaiters    *Waiters[Id, SettledTask]
	idleWaiters    *Waiters[idleKey, struct{}]
	enabled        bool
	closing        bool
	cascadePending bool
	reconcileDue   bool
}

// idleKey identifies an idle wait: one conversation, or the ownerless roots
// when Conversation is nil.
type idleKey struct {
	Conversation *Id
}

// NewTaskScheduler builds a scheduler. Call Open to load the live tasks.
func NewTaskScheduler(options TaskSchedulerOptions) *TaskScheduler {
	return &TaskScheduler{
		session: options.Session, storage: options.Storage, registry: options.Registry,
		now: options.Now, report: options.Report, settle: options.SettleOutcome,
		withdraw: options.WithdrawInputs, context: options.Context,
		mirror:      NewSchedulerMirror(),
		invocations: map[Id]*Invocation{},
		taskWaiters: &Waiters[Id, SettledTask]{},
		idleWaiters: &Waiters[idleKey, struct{}]{},
	}
}

// Context is the scheduler's commit context; it carries no caller
// cancellation.
func (s *TaskScheduler) Context() chord.Context {
	if s.context == nil {
		return context.Background()
	}
	return s.context
}

// Mirror is the scheduler's live mirror.
func (s *TaskScheduler) Mirror() *SchedulerMirror { return s.mirror }

// Open loads the live tasks and subscribes to the session's publications and
// close.
func (s *TaskScheduler) Open(ctx chord.Context) error {
	s.session.SubscribeCommits(func(publication CommitPublication, _ chord.Context) {
		s.Observe(publication)
	})
	s.session.SubscribeClose(func() { s.Seal() })
	if s.registry != nil {
		s.unsubscribeRegistry = s.registry.Subscribe(func() { s.scheduleReconcile() })
	}
	if err := s.session.Commit(ctx, func(tx *Transaction) error {
		return LoadLiveTasks(tx, s.mirror)
	}); err != nil {
		return err
	}
	s.mu.Lock()
	s.cascadePending = true
	s.mu.Unlock()
	s.scheduleReconcile()
	return nil
}

// Resume enables scheduling.
func (s *TaskScheduler) Resume() {
	s.mu.Lock()
	s.enabled = true
	s.mu.Unlock()
}

// Enabled reports whether scheduling is enabled.
func (s *TaskScheduler) Enabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled
}

// Closing reports whether the harness is closing.
func (s *TaskScheduler) Closing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closing
}

// Reserve reserves every eligible task in one commit: load the owner scopes,
// plan the reservations, orphan the abort-marked tasks no definition can take,
// stage the running states, and register an invocation per reserved task.
func (s *TaskScheduler) Reserve(ctx chord.Context) ([]*Invocation, error) {
	s.mu.Lock()
	if !s.enabled || s.closing {
		s.mu.Unlock()
		return nil, nil
	}
	s.mu.Unlock()
	registered := []*Invocation{}
	err := s.session.Commit(s.Context(), func(tx *Transaction) error {
		if _, err := LoadScopes(false, s.mirror, s.storage, s.Context()); err != nil {
			return err
		}
		records := s.LiveRecords()
		var snapshot RegistrySnapshot
		if s.registry != nil {
			snapshot = s.registry.Snapshot()
		}
		plans, orphans := PlanReservations(records, s.mirror.Graph(), snapshot, s.RunningInvocations(), s.mirror.FailedMigrations, s.report)
		for index := range orphans {
			reason := orphans[index].Reason
			outcome := &TaskOutcome{Status: OutcomeOrphaned, Reason: &reason}
			if err := TerminateTask(tx, records, orphans[index].Record, outcome, s.settle); err != nil {
				return err
			}
		}
		for _, plan := range plans {
			if plan.SetRunning != nil {
				if err := tx.SetTask(plan.SetRunning); err != nil {
					return err
				}
			}
			invocation := NewInvocation(plan.TaskID, plan.Record.ConversationID, plan.Mode, s.Context())
			s.mu.Lock()
			s.invocations[plan.TaskID] = invocation
			s.mu.Unlock()
			registered = append(registered, invocation)
		}
		return nil
	})
	if err != nil {
		s.mu.Lock()
		for _, invocation := range registered {
			if s.invocations[invocation.TaskID] == invocation {
				delete(s.invocations, invocation.TaskID)
			}
		}
		s.mu.Unlock()
		_ = ctx
		return nil, err
	}
	return registered, nil
}

// Observe applies one publication to the mirror and signals what changed.
func (s *TaskScheduler) Observe(publication CommitPublication) {
	s.mu.Lock()
	effects := s.mirror.Observe(publication)
	s.mu.Unlock()
	for _, record := range effects.Terminal {
		s.taskWaiters.Resolve(record.ID, SettledTask{TaskRecord: record})
	}
	for _, id := range effects.SignalInvocations {
		s.mu.Lock()
		invocation := s.invocations[id]
		s.mu.Unlock()
		if invocation != nil && invocation.Mode == "run" {
			invocation.Signal()
		}
	}
	if effects.ScheduleReconcile {
		s.scheduleReconcile()
	}
}

// Seal begins closing: reject the waiters and end every invocation.
func (s *TaskScheduler) Seal() {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return
	}
	s.closing = true
	s.mu.Unlock()
	if s.unsubscribeRegistry != nil {
		s.unsubscribeRegistry()
	}
	err := ClosedError()
	s.taskWaiters.RejectAll(err)
	s.idleWaiters.RejectAll(err)
	s.mu.Lock()
	invocations := make([]*Invocation, 0, len(s.invocations))
	for _, invocation := range s.invocations {
		invocations = append(invocations, invocation)
	}
	s.mu.Unlock()
	for _, invocation := range invocations {
		invocation.End()
	}
}

// Join waits for every invocation to end.
func (s *TaskScheduler) Join(ctx chord.Context) error {
	s.mu.Lock()
	invocations := make([]*Invocation, 0, len(s.invocations))
	for _, invocation := range s.invocations {
		invocations = append(invocations, invocation)
	}
	s.mu.Unlock()
	for _, invocation := range invocations {
		select {
		case <-invocation.Done():
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// GetTask reads a task: the live mirror first, then Storage.
func (s *TaskScheduler) GetTask(ctx chord.Context, id Id) (*TaskRecord, error) {
	s.mu.Lock()
	record, present := s.mirror.Live[id]
	s.mu.Unlock()
	if present {
		return &record, nil
	}
	return s.storage.Task(ctx, id)
}

// WaitForTask resolves with the task's terminal receipt; cancelling ctx cancels
// only this wait.
func (s *TaskScheduler) WaitForTask(ctx chord.Context, id Id) (SettledTask, error) {
	var await func() (SettledTask, error)
	err := s.session.ReadOnLine(func() error {
		s.mu.Lock()
		closing := s.closing
		_, present := s.mirror.Live[id]
		s.mu.Unlock()
		if closing {
			return ClosedError()
		}
		if present {
			await = s.taskWaiters.Register(id, ctx)
			return nil
		}
		record, err := s.storage.Task(ctx, id)
		if err != nil {
			return err
		}
		if record == nil {
			return fmt.Errorf("Task %d does not exist", id)
		}
		settled := SettledTask{TaskRecord: *record}
		await = func() (SettledTask, error) { return settled, nil }
		return nil
	})
	if err != nil {
		return SettledTask{}, err
	}
	return await()
}

// LiveRecords is the mirror's live records in ascending id order.
func (s *TaskScheduler) LiveRecords() []TaskRecord {
	s.mu.Lock()
	records := make([]TaskRecord, 0, len(s.mirror.Live))
	for _, record := range s.mirror.Live {
		records = append(records, record)
	}
	s.mu.Unlock()
	sort.Slice(records, func(left, right int) bool { return records[left].ID < records[right].ID })
	return records
}

// RunningInvocations is the set of task ids with a live invocation.
func (s *TaskScheduler) RunningInvocations() map[Id]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	running := make(map[Id]bool, len(s.invocations))
	for id := range s.invocations {
		running[id] = true
	}
	return running
}

// Inspect derives the point-in-time view of live work.
func (s *TaskScheduler) Inspect(snapshot RegistrySnapshot) HarnessInspection {
	records := s.LiveRecords()
	s.mu.Lock()
	closing, enabled := s.closing, s.enabled
	s.mu.Unlock()
	return BuildInspection(records, s.mirror, snapshot, s.RunningInvocations(), closing, enabled)
}

func (s *TaskScheduler) scheduleReconcile() {
	s.mu.Lock()
	s.reconcileDue = true
	s.mu.Unlock()
}
