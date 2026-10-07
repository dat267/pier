package interactive

import (
	"context"
	"testing"
	"testing/synctest"
	"time"
)

// fakeLoopHost stands in for the renderer and the raw terminal: the four things
// the loop's scheduling asks the outside world.
type fakeLoopHost struct {
	animCalls   int
	animNeeds   bool
	animDelay   time.Duration
	flushAt     time.Time
	flushed     int
	renderTick  chan struct{}
	beatWork    bool
	outputBytes int64
}

func (h *fakeLoopHost) PendingWriteBytes() int64 { return h.outputBytes }

func (h *fakeLoopHost) HasPendingBeatWork() bool { return h.beatWork }

func (h *fakeLoopHost) NextAnimation() (bool, time.Duration) {
	h.animCalls++
	return h.animNeeds, h.animDelay
}
func (h *fakeLoopHost) NextInputFlushDeadline() (time.Time, bool) {
	return h.flushAt, !h.flushAt.IsZero()
}
func (h *fakeLoopHost) FlushPendingInput() { h.flushed++ }
func (h *fakeLoopHost) RenderTicks() <-chan struct{} {
	if h.renderTick == nil {
		h.renderTick = make(chan struct{}, 1)
	}
	return h.renderTick
}
func (h *fakeLoopHost) pushRenderTick() { h.RenderTicks(); h.renderTick <- struct{}{} }

type scheduleHarness struct {
	host     *fakeLoopHost
	schedule *loopSchedule
	paints   int
	reads    int
}

func newScheduleHarness() *scheduleHarness {
	h := &scheduleHarness{host: &fakeLoopHost{}}
	h.schedule = newLoopSchedule(h.host,
		func() { h.paints++ },
		func() { h.reads++ },
	)
	return h
}

func (h *scheduleHarness) armed() bool { return h.schedule.arm() != nil }

// TestLoopScheduleArmsALiveAnimatorWithoutATurn is the regression test for the
// freeze: a tool's elapsed label can be built between the walk and the next
// paint, and only a paint invalidates the cached walk. With no turn active there
// is no fallback, so a stale "nothing animates" scan must not suppress the arm —
// the label would have nothing that repainted it.
func TestLoopScheduleArmsALiveAnimatorWithoutATurn(t *testing.T) {
	h := newScheduleHarness()
	h.host.animNeeds = false
	if h.armed() {
		t.Fatal("nothing animates, but the loop armed a tick")
	}
	if h.host.animCalls != 1 {
		t.Fatalf("walks = %d, want 1", h.host.animCalls)
	}

	// The tool appears. No paint follows before the scan's box expires.
	h.host.animNeeds = true
	h.host.animDelay = time.Second
	h.schedule.scanAt = time.Now().Add(-2 * time.Second)

	if !h.armed() {
		t.Fatal("a live animator was not armed: its label would freeze")
	}
	if h.host.animCalls != 2 {
		t.Fatalf("walks = %d, want 2 (the stale scan was not re-walked)", h.host.animCalls)
	}
}

// TestLoopScheduleTimeBoxesANothingAnimatesScan pins the cost side: a fresh
// "nothing animates" walk still short-circuits, so an input event that paints
// nothing does not pay for the walk (D164).
func TestLoopScheduleTimeBoxesANothingAnimatesScan(t *testing.T) {
	h := newScheduleHarness()
	h.host.animNeeds = false
	h.armed()
	h.armed()
	if h.host.animCalls != 1 {
		t.Fatalf("walks = %d, want 1: a fresh scan was re-walked", h.host.animCalls)
	}
	h.schedule.scanAt = time.Now().Add(-2 * time.Second)
	h.armed()
	if h.host.animCalls != 2 {
		t.Fatalf("walks = %d, want 2: a stale scan was not re-walked", h.host.animCalls)
	}
}

// TestLoopScheduleTicksWhileWorkIsActive pins the fallback that covers a walk
// which does not reach a freshly built label.
func TestLoopScheduleTicksWhileWorkIsActive(t *testing.T) {
	h := newScheduleHarness()
	h.host.animNeeds = false
	h.schedule.work.active = true
	h.schedule.scanValid = true
	h.schedule.scanNeeds = false
	h.schedule.scanAt = time.Now()

	if !h.armed() {
		t.Fatal("a running turn must tick even when the walk reports nothing")
	}
	if delay := time.Until(h.schedule.deadline); delay <= 0 || delay > 2*time.Second {
		t.Fatalf("tick delay = %v", delay)
	}
}

// TestLoopScheduleAPaintInvalidatesTheScan pins the invalidation contract: a
// paint is the only thing that can change a component's animation state.
func TestLoopScheduleAPaintInvalidatesTheScan(t *testing.T) {
	h := newScheduleHarness()
	h.host.animNeeds = false
	h.armed()
	if h.host.animCalls != 1 {
		t.Fatalf("walks = %d, want 1", h.host.animCalls)
	}
	h.schedule.paintNow()
	if h.paints != 1 {
		t.Fatalf("paints = %d, want 1", h.paints)
	}
	h.armed()
	if h.host.animCalls != 2 {
		t.Fatalf("walks = %d, want 2: a paint did not invalidate the scan", h.host.animCalls)
	}
}

// TestLoopScheduleSharesItsTimerWithTheInputFlushDeadline pins that a lone ESC or
// a split sequence is flushed on the same loop timer as animation.
func TestLoopScheduleSharesItsTimerWithTheInputFlushDeadline(t *testing.T) {
	h := newScheduleHarness()
	h.host.flushAt = time.Now().Add(50 * time.Millisecond)
	if !h.armed() {
		t.Fatal("an input flush deadline must arm the loop timer")
	}
	if !h.schedule.deadline.Equal(h.host.flushAt) {
		t.Fatalf("deadline = %v, want the flush deadline %v", h.schedule.deadline, h.host.flushAt)
	}

	// An already-expired deadline flushes now, on this beat, before painting.
	h2 := newScheduleHarness()
	h2.host.flushAt = time.Now().Add(-time.Second)
	h2.armed()
	if h2.host.flushed != 1 {
		t.Fatalf("flushes = %d, want 1 for an expired deadline", h2.host.flushed)
	}
}

// TestLoopScheduleCoalescesPaintsWithinTheFrameInterval pins that a burst of
// render ticks paints once per frame interval, and that a later one paints
// immediately.
func TestLoopScheduleCoalescesPaintsWithinTheFrameInterval(t *testing.T) {
	h := newScheduleHarness()
	h.schedule.paintNow()
	h.schedule.coalescePaint()
	if h.paints != 1 {
		t.Fatalf("paints = %d, want 1: a render tick inside the frame interval painted again", h.paints)
	}
	if h.schedule.paintChannel() == nil {
		t.Fatal("a coalesced paint was not scheduled")
	}
	<-h.schedule.paintChannel()
	h.schedule.paintNow()
	if h.paints != 2 {
		t.Fatalf("paints = %d, want 2", h.paints)
	}

	h.schedule.lastPaint = time.Now().Add(-time.Hour)
	h.schedule.coalescePaint()
	if h.paints != 3 {
		t.Fatalf("paints = %d, want 3: a render tick after the interval must paint now", h.paints)
	}
}

// TestLoopSchedulePaintsInputOnlyWhenARenderTickIsPending pins D164's rule: a
// keystroke that changed nothing asks for nothing and costs no frame.
func TestLoopSchedulePaintsInputOnlyWhenARenderTickIsPending(t *testing.T) {
	h := newScheduleHarness()
	h.schedule.paintIfRequested()
	if h.paints != 0 || h.reads != 0 {
		t.Fatalf("paints = %d reads = %d, want none without a pending render tick", h.paints, h.reads)
	}
	h.host.pushRenderTick()
	h.schedule.paintIfRequested()
	if h.paints != 1 || h.reads != 1 {
		t.Fatalf("paints = %d reads = %d, want one of each", h.paints, h.reads)
	}
}

// TestLoopScheduleDefersPaintsUntilOutputDrains pins D198: all paint paths
// retain a request while the console is backed up, rather than generating
// differential frames that cannot yet be delivered.
func TestLoopScheduleDefersPaintsUntilOutputDrains(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newScheduleHarness()
		defer h.schedule.close()
		h.host.outputBytes = 256 * 1024
		for i := 0; i < 100; i++ {
			h.host.pushRenderTick()
			h.schedule.paintIfRequested()
			h.schedule.paintNow() // resize and animation use this path too
			h.schedule.coalescePaint()
		}
		if h.paints != 0 {
			t.Fatalf("backed-up console generated %d frames, want none", h.paints)
		}
		if h.reads != 0 {
			t.Fatalf("tagged %d input paints that were never generated", h.reads)
		}
		if h.schedule.paintChannel() == nil {
			t.Fatal("deferred render request has no retry wake-up")
		}
		// Hysteresis prevents a nearly full writer from repeatedly repainting.
		h.host.outputBytes = 128*1024 + 1
		<-h.schedule.paintChannel()
		h.schedule.paintNow()
		if h.paints != 0 || h.schedule.paintChannel() == nil {
			t.Fatal("paint resumed before the low-water mark")
		}
		// No new render request is required after the writer catches up.
		h.host.outputBytes = 128 * 1024
		<-h.schedule.paintChannel()
		h.schedule.paintNow()
		if h.paints != 1 || h.reads != 1 || h.schedule.paintChannel() != nil {
			t.Fatalf("drained console paints=%d reads=%d retry=%v, want one final input paint and no retry", h.paints, h.reads, h.schedule.paintChannel())
		}
	})
}

func TestDeferredPaintRetryIsNotPostponedByRenderRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newScheduleHarness()
		defer h.schedule.close()
		h.host.outputBytes = 256 * 1024
		h.schedule.paintNow()
		for i := 0; i < 8; i++ {
			time.Sleep(time.Millisecond)
			h.host.pushRenderTick()
			h.schedule.paintIfRequested()
		}
		time.Sleep(8 * time.Millisecond)
		h.host.outputBytes = 0
		select {
		case <-h.schedule.paintChannel():
			h.schedule.paintNow()
		default:
			t.Fatal("render requests postponed the original 16 ms retry")
		}
		if h.paints != 1 {
			t.Fatalf("recovery paints = %d, want one", h.paints)
		}
	})
}

// TestLoopScheduleRunsOneWorkAtATime pins the work queue: at most one blocking
// unit runs, the rest queue, and finishing starts the next.
func TestLoopScheduleRunsOneWorkAtATime(t *testing.T) {
	h := newScheduleHarness()
	h.schedule.setContext(context.Background())
	release := make(chan struct{})
	done := make(chan string, 4)

	h.schedule.runWork(func(context.Context) error {
		<-release
		done <- "a"
		return nil
	})
	if !h.schedule.workActive() {
		t.Fatal("work did not start")
	}
	h.schedule.runWork(func(context.Context) error {
		done <- "b"
		return nil
	})
	if h.schedule.workActive() != true {
		t.Fatal("the second unit must queue, not run")
	}
	select {
	case got := <-done:
		t.Fatalf("a second unit ran concurrently (%q)", got)
	default:
	}

	close(release)
	if err := <-h.schedule.workDone(); err != nil {
		t.Fatalf("work error = %v", err)
	}
	if got := <-done; got != "a" {
		t.Fatalf("first unit = %q, want a", got)
	}
	h.schedule.finishWork()
	if got := <-done; got != "b" {
		t.Fatalf("next unit = %q, want b", got)
	}
	h.schedule.finishWork()
	if h.schedule.workActive() {
		t.Fatal("work still active after the queue drained")
	}
}

// TestLoopScheduleFlushesAndPaintsOnAnimationFire pins the fire path: the
// deadline clears so the next iteration re-walks.
func TestLoopScheduleFlushesAndPaintsOnAnimationFire(t *testing.T) {
	h := newScheduleHarness()
	h.host.animNeeds = true
	h.host.animDelay = time.Second
	h.armed()
	h.schedule.animationFired()
	if !h.schedule.deadline.IsZero() {
		t.Fatalf("deadline = %v, want cleared", h.schedule.deadline)
	}
	if !h.armed() {
		t.Fatal("the loop did not re-arm after the frame fired")
	}
}
