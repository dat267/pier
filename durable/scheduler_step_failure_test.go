package durable

import (
	"context"
	"errors"
	"testing"
)

// Port of scheduler.ts's step catch (v1.2.0): a throw in the scheduler's own
// commit ends the invocation and fails the Session, so the task is not reserved
// and run again in the same process.
func TestStepCommitFailureFailsSession(t *testing.T) {
	session, storage := newRootSession(t)
	scheduler := newTestScheduler(t, session, storage)
	taskID := Id(42)
	scheduler.mirror.Live[taskID] = TaskRecord{
		ID: taskID, ConversationID: RootConversationID, Kind: "test.step-failure",
		State: TaskState{Status: TaskRunning},
	}
	invocation := NewInvocation(taskID, RootConversationID, "run", context.Background())
	_, _, err := scheduler.step(invocation, func(tx *Transaction, current TaskRecord) (PhaseDecision, error) {
		return PhaseDecision{}, errors.New("step commit failed")
	})
	if err == nil {
		t.Fatal("step returned no error")
	}
	if session.Failure() == nil {
		t.Fatal("a failed scheduler step did not fail the Session")
	}
}
