package durable

import (
	"testing"
)

// Port of the scheduler's live-mirror publication observer.

func taskPublication(records ...TaskRecord) CommitPublication {
	publication := CommitPublication{}
	for index := range records {
		publication.Changes = append(publication.Changes, CommitChange{
			Write: &StorageWrite{Type: "task", Task: &records[index]},
		})
	}
	return publication
}

func conversationPublication(record ConversationRecord) CommitPublication {
	return CommitPublication{Changes: []CommitChange{{Write: &StorageWrite{Type: "conversation", Conversation: &record}}}}
}

func submissionPublication(record SubmissionRecord) CommitPublication {
	return CommitPublication{Changes: []CommitChange{{Write: &StorageWrite{Type: "submission", Submission: &record}}}}
}

func TestSchedulerMirrorTracksLiveAndTerminal(t *testing.T) {
	mirror := NewSchedulerMirror()
	pending := traversalRecord(1, nil, false, TaskPending)
	effects := mirror.Observe(taskPublication(pending))
	if len(effects.Updated) != 1 || mirror.Live[1].ID != 1 {
		t.Fatalf("effects = %+v, live = %+v", effects, mirror.Live)
	}
	mirror.ConversationOwners[1] = true
	terminal := traversalRecord(1, nil, false, TaskTerminal)
	effects = mirror.Observe(taskPublication(terminal))
	if len(effects.Terminal) != 1 || effects.Terminal[0].ID != 1 || !effects.ScheduleReconcile {
		t.Fatalf("effects = %+v", effects)
	}
	if _, present := mirror.Live[1]; present {
		t.Fatal("a terminal task leaves the live mirror")
	}
	if node, present := mirror.Settled[1]; !present || node.ConversationID != 1 {
		t.Fatalf("settled = %+v", node)
	}
}

func TestSchedulerMirrorSignalsAbortMark(t *testing.T) {
	mirror := NewSchedulerMirror()
	mirror.Observe(taskPublication(traversalRecord(1, nil, false, TaskRunning)))
	marked := traversalRecord(1, nil, false, TaskRunning)
	marked.AbortRequested = true
	effects := mirror.Observe(taskPublication(marked))
	if !effects.CascadePending || len(effects.SignalInvocations) != 1 || effects.SignalInvocations[0] != 1 {
		t.Fatalf("effects = %+v", effects)
	}
	// Re-observing the same mark does not signal again.
	effects = mirror.Observe(taskPublication(marked))
	if len(effects.SignalInvocations) != 0 {
		t.Fatalf("effects = %+v", effects)
	}
}

func TestSchedulerMirrorCompletingIntent(t *testing.T) {
	mirror := NewSchedulerMirror()
	completed := traversalRecord(1, nil, false, TaskCompleting)
	completed.State.Outcome = &TaskOutcome{Status: OutcomeCompleted}
	effects := mirror.Observe(taskPublication(completed))
	if !effects.ScheduleReconcile || effects.CascadePending {
		t.Fatalf("effects = %+v", effects)
	}
	failed := traversalRecord(2, nil, false, TaskCompleting)
	failed.AbortRequested = true
	failed.State.Outcome = &TaskOutcome{Status: OutcomeFaulted}
	effects = mirror.Observe(taskPublication(failed))
	if !effects.CascadePending || !effects.ScheduleReconcile {
		t.Fatalf("effects = %+v", effects)
	}
}

func TestSchedulerMirrorFailFastWaiter(t *testing.T) {
	mirror := NewSchedulerMirror()
	waiter := traversalRecord(2, nil, false, TaskWaiting)
	waiter.State.On = []Id{1}
	waiter.State.Policy = JoinFailFast
	effects := mirror.Observe(taskPublication(waiter))
	if !mirror.FailFastChecks[2] || !effects.ScheduleReconcile {
		t.Fatalf("checks = %+v, effects = %+v", mirror.FailFastChecks, effects)
	}
	// A member that fails re-adds the waiter to the checks.
	delete(mirror.FailFastChecks, 2)
	failed := traversalRecord(1, nil, false, TaskCompleting)
	failed.State.Outcome = &TaskOutcome{Status: OutcomeFailed}
	effects = mirror.Observe(taskPublication(failed))
	if !mirror.FailFastChecks[2] || !effects.ScheduleReconcile {
		t.Fatalf("checks = %+v, effects = %+v", mirror.FailFastChecks, effects)
	}
}

func TestSchedulerMirrorLearnsEdges(t *testing.T) {
	mirror := NewSchedulerMirror()
	effects := mirror.Observe(conversationPublication(ConversationRecord{ID: 1}))
	owner, present := effects.NewEdges[1]
	if !present || owner != nil {
		t.Fatalf("edges = %+v", effects.NewEdges)
	}
	// A known edge is not learned again.
	effects = mirror.Observe(conversationPublication(ConversationRecord{ID: 1}))
	if len(effects.NewEdges) != 0 {
		t.Fatalf("edges = %+v", effects.NewEdges)
	}
}

func TestSchedulerMirrorQueuedInputCascade(t *testing.T) {
	mirror := NewSchedulerMirror()
	intent := traversalRecord(5, nil, false, TaskRunning)
	intent.ConversationID = 2
	intent.AbortRequested = true
	mirror.Observe(taskPublication(intent))
	mirror.Observe(conversationPublication(ConversationRecord{ID: 1, Owner: &ConversationOwner{ConversationID: 2, TaskID: 5}}))
	effects := mirror.Observe(submissionPublication(SubmissionRecord{
		ID: 1, ConversationID: 1, Type: SubmissionTypeInput, Status: SubmissionQueued,
	}))
	if !effects.CascadePending {
		t.Fatalf("effects = %+v", effects)
	}
}
