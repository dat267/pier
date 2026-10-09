package durable

import (
	"fmt"

	"github.com/dat267/pier/chord"
)

// Port of the scheduler's drain/reconcile loop and its abort/idle methods
// (harness/scheduler.ts).

// Kick marks the scheduler dirty and drains it if it can.
func (s *TaskScheduler) Kick() {
	s.mu.Lock()
	s.dirty = true
	if s.draining || !s.enabled || s.closing {
		s.mu.Unlock()
		return
	}
	s.draining = true
	s.mu.Unlock()
	go s.drain()
}

func (s *TaskScheduler) drain() {
	defer func() {
		s.mu.Lock()
		s.draining = false
		dirty := s.dirty
		s.mu.Unlock()
		// A wakeup that arrived during a failed pass still needs its pass.
		if dirty {
			s.Kick()
		}
	}()
	for {
		s.mu.Lock()
		dirty, enabled, closing := s.dirty, s.enabled, s.closing
		if dirty {
			s.dirty = false
		}
		s.mu.Unlock()
		if !dirty || !enabled || closing {
			return
		}
		reservations, err := s.Reserve(s.Context())
		if err != nil {
			if !s.Closing() && s.report != nil {
				s.report(err)
			}
			return
		}
		for _, reservation := range reservations {
			s.StartInvocation(reservation)
		}
	}
}

// reconcile applies what committed records imply: abort marks below live owners
// with cancellation intent, failFast marks, withdrawn queued inputs, and the
// terminal record of every free completing task.
func (s *TaskScheduler) reconcile() {
	s.mu.Lock()
	s.reconcileScheduled = false
	cascade := s.cascadePending
	s.cascadePending = false
	checks := make([]Id, 0, len(s.mirror.FailFastChecks))
	for id := range s.mirror.FailFastChecks {
		checks = append(checks, id)
	}
	s.mirror.FailFastChecks = map[Id]bool{}
	s.mu.Unlock()
	s.mu.Lock()
	queued, loadErr := LoadScopes(cascade, s.mirror, s.storage, s.Context())
	records := s.liveRecordsUnlocked()
	graph := s.mirror.Graph()
	s.mu.Unlock()
	if loadErr != nil {
		if !s.Closing() && s.report != nil {
			s.report(loadErr)
		}
		s.resolveIdleWaiters()
		return
	}
	err := s.session.Commit(s.Context(), func(tx *Transaction) error {
		if s.Closing() {
			return nil
		}
		load := func(id Id) (*TaskRecord, error) { return s.storage.Task(s.Context(), id) }
		return ReconcileTasks(tx, records, graph, queued, checks, load, s.withdraw, s.settle)
	})
	if err != nil {
		// Any pass may have staged marks, so a failed one is retried with the
		// next commit.
		s.mu.Lock()
		s.cascadePending = true
		for _, id := range checks {
			s.mirror.FailFastChecks[id] = true
		}
		s.mu.Unlock()
		if !s.Closing() && s.report != nil {
			s.report(err)
		}
	}
	s.resolveIdleWaiters()
}

func (s *TaskScheduler) resolveIdleWaiters() {
	for _, key := range s.idleWaiters.Keys() {
		if s.isIdle(key) {
			s.idleWaiters.Resolve(key, struct{}{})
		}
	}
	s.refreshContextRetention()
}

func (s *TaskScheduler) isIdle(key idleKey) bool {
	return Idle(key.Conversation, s.LiveRecords(), s.mirror.Graph())
}

// WaitForIdle resolves when ordinary traversal from the conversation, or from
// every ownerless conversation, reaches no live non-background task.
func (s *TaskScheduler) WaitForIdle(conversationID *Id, ctx chord.Context) error {
	if s.Closing() {
		return ClosedError()
	}
	key := idleKey{Conversation: conversationID}
	// Register before the idle check: a drain that turns the scope idle can no
	// longer settle between the check and the registration, which left the
	// waiter parked forever under load. If the scope is already idle (or a
	// concurrent drain just made it idle), resolve it now; otherwise ensure a
	// reconcile runs to wake it.
	settle := s.idleWaiters.Register(key, ctx)
	if s.isIdle(key) {
		s.idleWaiters.Resolve(key, struct{}{})
	} else {
		s.scheduleReconcile()
	}
	_, err := settle()
	return err
}

// AbortConversation withdraws the queued inputs and marks every live
// non-background task that ordinary traversal from the conversation reaches;
// it resolves once the scope is idle.
func (s *TaskScheduler) AbortConversation(conversationID Id, background bool, ctx chord.Context) error {
	reached := []Id{}
	s.mu.Lock()
	queued, loadErr := LoadScopes(true, s.mirror, s.storage, s.Context())
	records := s.liveRecordsUnlocked()
	graph := s.mirror.Graph()
	s.mu.Unlock()
	if loadErr != nil {
		return loadErr
	}
	err := s.session.Commit(s.Context(), func(tx *Transaction) error {
		scope := SchedulerScope{Conversation: &conversationID}
		for _, record := range records {
			if record.Background && !background {
				continue
			}
			if inScope, known := InScope(ParentOf(NodeOf(record)), scope, background, graph); !known || !inScope {
				continue
			}
			reached = append(reached, record.ID)
			if !record.AbortRequested {
				replacement := record
				replacement.AbortRequested = true
				if err := tx.SetTask(&replacement); err != nil {
					return err
				}
			}
		}
		for _, id := range queued {
			conversation := id
			if inScope, known := InScope(SchedulerUp{Conversation: &conversation}, scope, background, graph); known && inScope {
				if err := s.withdraw(tx, id); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if background {
		for _, id := range reached {
			if _, err := s.WaitForTask(ctx, id); err != nil {
				return err
			}
		}
	}
	return s.WaitForIdle(&conversationID, ctx)
}

// Abort commits the abort mark, or settles a task no definition can take as
// orphaned when nothing it owns is live, then joins the run invocation.
func (s *TaskScheduler) Abort(id Id, ctx chord.Context) (string, error) {
	var plan AbortPlan
	err := s.session.Commit(s.Context(), func(tx *Transaction) error {
		current, err := tx.Task(id)
		if err != nil {
			return err
		}
		if current == nil {
			return fmt.Errorf("Task %d does not exist", id)
		}
		s.mu.Lock()
		invocation := s.invocations[id]
		s.mu.Unlock()
		ownedHas := false
		var resolution *SchedulerResolution
		var migrationErr error
		if invocation == nil && current.State.Status != TaskCompleting {
			s.mu.Lock()
			if _, err := LoadScopes(false, s.mirror, s.storage, s.Context()); err != nil {
				s.mu.Unlock()
				return err
			}
			ownedHas = OwnedTaskIDs(s.liveRecordsUnlocked(), s.mirror.Graph())[id]
			if !ownedHas {
				resolved := ResolveRecord(*current, s.RegistrySnapshot(), s.mirror.FailedMigrations, func(err error) { migrationErr = err })
				resolution = &resolved
			}
			s.mu.Unlock()
		}
		if migrationErr != nil && s.report != nil {
			s.report(migrationErr)
		}
		plan = PlanAbort(*current, invocation, ownedHas, resolution)
		if plan.Orphan != nil {
			outcome := &TaskOutcome{Status: OutcomeOrphaned, Reason: plan.Orphan.Reason}
			return TerminateTask(tx, s.LiveRecords(), *current, outcome, s.settle)
		}
		if plan.Mark {
			replacement := *current
			replacement.AbortRequested = true
			return tx.SetTask(&replacement)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if plan.JoinRun != nil {
		select {
		case <-plan.JoinRun.Done():
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return plan.Result, nil
}
