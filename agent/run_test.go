package agent

import (
	"context"
	"sync"
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

// Reference: pi v1.4.0 agent.ts finishRun releases runtime-owned run state.
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

// A481: runWithLifecycle used to publish activeRun = nil and unlock before it
// called runContexts.Delete(a) on a process-wide map. A run that started in
// that window stored its own context, and the older run's Delete then removed
// it, so a live run observed a nil context. The context now lives on activeRun
// itself. Reference: upstream has no equivalent map; the invariant is
// local, "a live run always has its context".
func TestRunContextIsPresentForTheWholeRun(t *testing.T) {
	a, err := NewAgent(&AgentOptions{StreamFn: unusedStreamFn()})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	var lost atomic.Int64
	exec := func(context.Context) error {
		if a.runContext() == nil {
			lost.Add(1)
		}
		return nil
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 40000; i++ {
				_ = a.runWithLifecycle(context.Background(), exec)
			}
		}()
	}
	wg.Wait()
	if n := lost.Load(); n != 0 {
		t.Fatalf("runContext() was nil for a live run %d times", n)
	}
}
