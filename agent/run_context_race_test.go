package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

// A481: runWithLifecycle publishes activeRun = nil and unlocks before it calls
// runContexts.Delete(a). A run that starts in that window stores its own
// context, and the older run's Delete then removes it, so a live run observes a
// nil context. Reference: upstream has no equivalent map; the invariant is
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
