package agent

import (
	"context"
	"errors"
	"testing"
)

// Reference: pi v1.4.0 agent.ts runWithLifecycle awaits handleRunFailure;
// an error from failure-event listeners rejects the prompt.
func TestFailureLifecycleListenerErrorReachesCaller(t *testing.T) {
	a, err := NewAgent(&AgentOptions{StreamFn: unusedStreamFn()})
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("listener failed")
	a.Subscribe(func(AgentEvent, context.Context) error { return want })
	if err := a.PromptText(context.Background(), "x"); !errors.Is(err, want) {
		t.Fatalf("error=%v; want %v", err, want)
	}
	if a.State().IsStreaming {
		t.Fatal("run not settled")
	}
}
