package agent

import (
	"context"
	"sync/atomic"
	"testing"
)

type observingRunParent struct {
	context.Context
	done                chan struct{}
	registered, stopped atomic.Int32
}

func (p *observingRunParent) Done() <-chan struct{} { return p.done }
func (p *observingRunParent) AfterFunc(func()) func() bool {
	p.registered.Add(1)
	return func() bool { p.stopped.Add(1); return true }
}

// Reference: pi v1.3.0 agent.ts finishRun releases runtime-owned run state.
// Go additionally owns the registration on a caller's cancellation context.
func TestFinishedRunReleasesParentCancellation(t *testing.T) {
	p := &observingRunParent{Context: context.Background(), done: make(chan struct{})}
	a, err := NewAgent(&AgentOptions{StreamFn: unusedStreamFn()})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := a.PromptText(p, "hello"); err != nil {
			t.Fatal(err)
		}
	}
	if p.registered.Load() != 3 || p.stopped.Load() != 3 {
		t.Fatalf("registered=%d stopped=%d", p.registered.Load(), p.stopped.Load())
	}
	if a.State().IsStreaming {
		t.Fatal("run is still active")
	}
}
