package durable

import (
	"context"
	"testing"
	"time"
)

// Port of the scheduler's invocation lifecycle and gate.

type fakeWatch struct {
	stopped   bool
	stopCount int
}

func (w *fakeWatch) Stop() WatchEnd {
	w.stopped = true
	w.stopCount++
	return WatchEnd{}
}

func TestInvocationLifecycle(t *testing.T) {
	invocation := NewInvocation(7, 3, "run", nil)
	if invocation.Ended() || invocation.AssertLive() != nil {
		t.Fatal("a fresh invocation is live")
	}
	watch := &fakeWatch{}
	invocation.AddWatch(watch)
	invocation.End()
	if !watch.stopped || watch.stopCount != 1 {
		t.Fatalf("watch = %+v", watch)
	}
	if !invocation.Ended() {
		t.Fatal("the invocation ended")
	}
	if err := invocation.AssertLive(); err == nil {
		t.Fatal("an ended invocation rejects")
	} else if ended, ok := err.(*EndedInvocationError); !ok || ended.TaskID != 7 {
		t.Fatalf("err = %v", err)
	}
	select {
	case <-invocation.Done():
	case <-time.After(time.Second):
		t.Fatal("done did not settle")
	}
	if err := invocation.Context().Err(); err == nil {
		t.Fatal("the context is cancelled")
	}
	// End is idempotent and a watch added afterwards stops at once.
	invocation.End()
	late := &fakeWatch{}
	invocation.AddWatch(late)
	if !late.stopped {
		t.Fatal("a late watch stops at once")
	}
}

func TestGateTask(t *testing.T) {
	invocation := NewInvocation(7, 3, "run", context.Background())
	running := &TaskRecord{ID: 7, State: TaskState{Status: TaskRunning}}
	record, err := GateTask(invocation, running, false)
	if err != nil || record.ID != 7 {
		t.Fatalf("record = %+v, %v", record, err)
	}
	if _, err := GateTask(invocation, nil, false); err == nil || err.Error() != "Task 7 is terminal" {
		t.Fatalf("err = %v", err)
	}
	waiting := &TaskRecord{ID: 7, State: TaskState{Status: TaskWaiting}}
	if _, err := GateTask(invocation, waiting, false); err == nil || err.Error() != "Task 7 is waiting" {
		t.Fatalf("err = %v", err)
	}
	marked := &TaskRecord{ID: 7, AbortRequested: true, State: TaskState{Status: TaskRunning}}
	if _, err := GateTask(invocation, marked, false); err == nil || err.Error() != "Task 7 has a durable abort mark" {
		t.Fatalf("err = %v", err)
	}
	// An abort invocation may proceed under an abort mark.
	abortInvocation := NewInvocation(7, 3, "abort", context.Background())
	if _, err := GateTask(abortInvocation, marked, false); err != nil {
		t.Fatalf("err = %v", err)
	}
	if _, err := GateTask(invocation, running, true); err == nil || err.Error() != "Harness is closed" {
		t.Fatalf("err = %v", err)
	}
	invocation.End()
	if _, err := GateTask(invocation, running, false); err == nil {
		t.Fatal("an ended invocation rejects")
	}
}
