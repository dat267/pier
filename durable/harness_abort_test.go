package durable

import (
	"context"
	"testing"
)

// Port of the scheduler's abort decision.

func TestPlanAbort(t *testing.T) {
	terminal := traversalRecord(1, nil, false, TaskTerminal)
	if plan := PlanAbort(terminal, nil, false, nil); plan.Result != "terminal" || plan.Mark || plan.Orphan != nil {
		t.Fatalf("plan = %+v", plan)
	}
	running := traversalRecord(2, nil, false, TaskRunning)
	invocation := NewInvocation(2, 1, "run", context.Background())
	plan := PlanAbort(running, invocation, false, nil)
	if plan.Result != "marked" || !plan.Mark || plan.JoinRun != invocation || plan.Orphan != nil {
		t.Fatalf("plan = %+v", plan)
	}
	// An abort invocation is not joined.
	abortInvocation := NewInvocation(2, 1, "abort", context.Background())
	if plan := PlanAbort(running, abortInvocation, false, nil); plan.JoinRun != nil {
		t.Fatalf("plan = %+v", plan)
	}
	// An already-marked task is not marked again.
	marked := running
	marked.AbortRequested = true
	if plan := PlanAbort(marked, invocation, false, nil); plan.Mark {
		t.Fatalf("plan = %+v", plan)
	}
	blocked := &SchedulerResolution{Kind: SchedulerBlocked, Reason: BlockedMissingTask}
	// A completing task is only marked, never orphaned.
	completing := traversalRecord(3, nil, false, TaskCompleting)
	if plan := PlanAbort(completing, nil, false, blocked); plan.Orphan != nil || !plan.Mark {
		t.Fatalf("plan = %+v", plan)
	}
	// A task no definition can take, owning nothing, settles as orphaned.
	pending := traversalRecord(4, nil, false, TaskPending)
	plan = PlanAbort(pending, nil, false, blocked)
	if plan.Orphan == nil || plan.Orphan.Status != OutcomeOrphaned ||
		plan.Orphan.Reason == nil || *plan.Orphan.Reason != BlockedMissingTask {
		t.Fatalf("plan = %+v", plan)
	}
	// Live owned work keeps it from being orphaned.
	if plan := PlanAbort(pending, nil, true, blocked); plan.Orphan != nil {
		t.Fatalf("plan = %+v", plan)
	}
	// A reservable definition keeps it from being orphaned.
	if plan := PlanAbort(pending, nil, false, &SchedulerResolution{Kind: SchedulerReady}); plan.Orphan != nil {
		t.Fatalf("plan = %+v", plan)
	}
}
