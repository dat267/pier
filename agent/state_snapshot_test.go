package agent

import (
	"context"
	"testing"
)

// Reference: pi v1.4.0 agent.ts state/processEvents run on one JS thread.
// A Go snapshot must not expose the map that later tool events mutate.
func TestStatePendingCallsSnapshotIsIndependent(t *testing.T) {
	a, err := NewAgent(&AgentOptions{StreamFn: unusedStreamFn()})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot AgentState
	a.Subscribe(func(e AgentEvent, _ context.Context) error {
		if e.Type == ToolExecutionStart {
			snapshot = a.State()
		}
		return nil
	})
	if err := a.processEvents(AgentEvent{Type: ToolExecutionStart, ToolCallID: "call"}, context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.processEvents(AgentEvent{Type: ToolExecutionEnd, ToolCallID: "call"}, context.Background()); err != nil {
		t.Fatal(err)
	}
	if !snapshot.PendingToolCalls["call"] {
		t.Fatal("tool end mutated the earlier snapshot")
	}
	delete(snapshot.PendingToolCalls, "call")
	if len(a.State().PendingToolCalls) != 0 {
		t.Fatal("unexpected active calls")
	}
}
