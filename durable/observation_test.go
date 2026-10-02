package durable

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
	"github.com/dat267/pier/chord/services"
)

// Go-only tests for session/observation.ts: the state-source bridge and the
// serialized watch. Upstream exercises them through the session and harness
// suites; the expectations here come from the observation source itself
// (CommitedStateSource/CommittedWatch) and the watch contract in types.ts.

func TestCommittedStateSourcePublishesFrames(t *testing.T) {
	type stateValue map[string]any
	source := NewCommittedStateSource[stateValue](stateValue{"value": float64(0)}, nil)
	attachment := source.Attach()
	// The snapshot is captured at attachment time, before activation.
	snapshot, cursor := attachment.Snapshot()
	if snapshot["value"] != float64(0) || cursor != 0 {
		t.Fatalf("snapshot = %+v cursor = %d", snapshot, cursor)
	}
	var frames []services.SourceFrame[stateValue]
	attachment.Activate(func(frame services.SourceFrame[stateValue]) {
		frames = append(frames, frame)
	})
	source.Advance(stateValue{"value": float64(1)}, []delta.Op{{Verb: delta.VerbSet, Path: delta.Path{"value"}, Value: float64(1)}}, context.Background())
	source.Advance(stateValue{"value": float64(2)}, []delta.Op{{Verb: delta.VerbSet, Path: delta.Path{"value"}, Value: float64(2)}}, context.Background())
	if len(frames) != 2 || frames[0].Cursor != 1 || frames[1].Cursor != 2 {
		t.Fatalf("frames = %+v", frames)
	}
	if frames[1].Value["value"] != float64(2) {
		t.Fatalf("frame value = %+v", frames[1].Value)
	}
	// Retirement publishes the null frame and stops accepting advances.
	source.Advance(nil, RetirementOperations, context.Background())
	if len(frames) != 3 || frames[2].Value != nil {
		t.Fatalf("frames = %+v", frames)
	}
	source.Advance(stateValue{"value": float64(3)}, nil, context.Background())
	if len(frames) != 3 {
		t.Fatalf("advance after retirement = %+v", frames)
	}
}

func TestCommittedStateSourceLifecycle(t *testing.T) {
	type stateValue map[string]any
	var releases int32
	source := NewCommittedStateSource[stateValue](stateValue{}, func() { atomic.AddInt32(&releases, 1) })
	first := source.Attach()
	second := source.Attach()
	first.Dispose()
	if atomic.LoadInt32(&releases) != 0 {
		t.Fatal("released before the last attachment")
	}
	second.Dispose()
	if atomic.LoadInt32(&releases) != 1 {
		t.Fatalf("releases = %d", releases)
	}
	// Disposal is idempotent and CloseSession releases at most once.
	second.Dispose()
	source.CloseSession()
	if atomic.LoadInt32(&releases) != 1 {
		t.Fatalf("releases after close = %d", releases)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("attaching to a closed source must panic")
		}
	}()
	source.Attach()
}

func TestCommittedWatchDeliversFrames(t *testing.T) {
	type stateValue map[string]any
	watch := NewCommittedWatch[stateValue](stateValue{"value": float64(0)}, nil, nil)
	var mu sync.Mutex
	var seen []int
	delivered := make(chan struct{}, 2)
	watch.Start(func(current stateValue, _ []delta.Op, _ chord.Context) error {
		mu.Lock()
		seen = append(seen, int(current["value"].(float64)))
		mu.Unlock()
		delivered <- struct{}{}
		return nil
	})
	watch.Advance(stateValue{"value": float64(1)}, []delta.Op{{Verb: delta.VerbSet, Path: delta.Path{"value"}, Value: float64(1)}}, context.Background())
	<-delivered
	watch.Advance(stateValue{"value": float64(2)}, []delta.Op{{Verb: delta.VerbSet, Path: delta.Path{"value"}, Value: float64(2)}}, context.Background())
	<-delivered
	mu.Lock()
	got := append([]int(nil), seen...)
	mu.Unlock()
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("seen = %v", got)
	}
	// Stop settles the watch and reports the reason.
	if end := watch.Stop(); end.Reason != WatchReasonStopped {
		t.Fatalf("end = %+v", end)
	}
	// A stopped watch ignores later frames and Stop stays idempotent.
	watch.Advance(stateValue{"value": float64(3)}, nil, context.Background())
	if end := watch.Stop(); end.Reason != WatchReasonStopped {
		t.Fatalf("second stop = %+v", end)
	}
}

// TestCommittedWatchRetiresOnNull covers the terminal null frame.
func TestCommittedWatchRetiresOnNull(t *testing.T) {
	type stateValue map[string]any
	watch := NewCommittedWatch[stateValue](stateValue{"value": float64(0)}, nil, nil)
	var retired int32
	watch.Start(func(current stateValue, _ []delta.Op, _ chord.Context) error {
		if current == nil {
			atomic.AddInt32(&retired, 1)
		}
		return nil
	})
	watch.Advance(nil, RetirementOperations, context.Background())
	<-watch.Closed()
	if end, _ := watch.End(); end.Reason != WatchReasonRetired {
		t.Fatalf("end = %+v", end)
	}
	if atomic.LoadInt32(&retired) != 1 {
		t.Fatal("the listener did not observe the retirement")
	}
}

// TestCommittedWatchListenerError covers the listener_error terminal.
func TestCommittedWatchListenerError(t *testing.T) {
	type stateValue map[string]any
	watch := NewCommittedWatch[stateValue](stateValue{}, nil, nil)
	watch.Start(func(_ stateValue, _ []delta.Op, _ chord.Context) error {
		return errors.New("listener failed")
	})
	watch.Advance(stateValue{"value": float64(1)}, nil, context.Background())
	<-watch.Closed()
	end, _ := watch.End()
	if end.Reason != WatchReasonListenerError || end.Err == nil || end.Err.Error() != "listener failed" {
		t.Fatalf("end = %+v", end)
	}
}

// TestCommittedWatchOverflowCollapses covers the bounded pending window: an
// unavailable listener keeps the newest replacement only, and later frames
// append after it.
func TestCommittedWatchOverflowCollapses(t *testing.T) {
	type stateValue map[string]any
	// The watch is not started, so every frame stays pending.
	watch := NewCommittedWatch[stateValue](stateValue{}, nil, nil)
	for i := 0; i < maxPendingWatchFrames+5; i++ {
		watch.Advance(stateValue{"value": float64(i)}, []delta.Op{{Verb: delta.VerbSet, Path: delta.Path{"value"}, Value: float64(i)}}, context.Background())
	}
	delivered := make(chan struct{})
	var firstOps []delta.Op
	var last atomic.Int64
	var frames int32
	watch.Start(func(current stateValue, ops []delta.Op, _ chord.Context) error {
		if atomic.AddInt32(&frames, 1) == 1 {
			firstOps = ops
			close(delivered)
		}
		last.Store(int64(current["value"].(float64)))
		return nil
	})
	select {
	case <-delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not run")
	}
	if len(firstOps) != 1 || firstOps[0].Verb != delta.VerbReplace {
		t.Fatalf("first ops = %+v", firstOps)
	}
	// The pending window holds maxPendingWatchFrames (100) advances; the 101st
	// collapses the queue into one replacement frame, and the four advances
	// after it append normally, so five frames are delivered in total.
	const wantFrames = int32(5)
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&frames) < wantFrames && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := atomic.LoadInt32(&frames); got != wantFrames {
		t.Fatalf("frames = %d last = %d", got, last.Load())
	}
	if last.Load() != int64(maxPendingWatchFrames+4) {
		t.Fatalf("last = %d", last.Load())
	}
	watch.Stop()
}

// TestCommittedWatchCancellation covers observeCancellation and closeSession.
func TestCommittedWatchCancellation(t *testing.T) {
	type stateValue map[string]any
	ctx, cancel := context.WithCancel(context.Background())
	watch := NewCommittedWatch[stateValue](stateValue{}, nil, nil)
	watch.ObserveCancellation(ctx)
	cancel()
	<-watch.Closed()
	if end, _ := watch.End(); end.Reason != WatchReasonCancelled {
		t.Fatalf("end = %+v", end)
	}

	closed := NewCommittedWatch[stateValue](stateValue{}, nil, nil)
	closed.CloseSession()
	<-closed.Closed()
	if end, _ := closed.End(); end.Reason != WatchReasonSessionClosed {
		t.Fatalf("end = %+v", end)
	}
}

// TestCommittedWatchStartNeverInline covers the "never invokes it inline"
// rule: a frame queued before start is delivered on the watch's own goroutine.
func TestCommittedWatchStartNeverInline(t *testing.T) {
	type stateValue map[string]any
	watch := NewCommittedWatch[stateValue](stateValue{"value": float64(0)}, nil, nil)
	watch.Advance(stateValue{"value": float64(1)}, nil, context.Background())
	var inline atomic.Bool
	inline.Store(true)
	done := make(chan struct{})
	watch.Start(func(_ stateValue, _ []delta.Op, _ chord.Context) error {
		if inline.Load() {
			t.Error("listener ran inline with start")
		}
		close(done)
		return nil
	})
	inline.Store(false)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not run")
	}
}
