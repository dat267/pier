package interactive

import (
	"context"
	"runtime"
	"testing"
	"testing/synctest"
	"weak"
)

// D143's loop-owned work queue adapts the awaited interactive-mode.ts work to
// Go workers. A completed queued unit must release its captured prompt data;
// only current/pending work owns those snapshots, not consumed backing slots.
func TestLoopWorkReleasesCompletedCaptures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		harness := newScheduleHarness()
		defer harness.schedule.close()
		firstStarted, releaseFirst := make(chan struct{}), make(chan struct{})
		harness.schedule.runWork(func(context.Context) error {
			close(firstStarted)
			<-releaseFirst
			return nil
		})
		<-firstStarted
		payload := queueCapturedLoopWork(harness.schedule)
		nextStarted, releaseNext := make(chan struct{}), make(chan struct{})
		harness.schedule.runWork(func(context.Context) error {
			close(nextStarted)
			<-releaseNext
			return nil
		})
		close(releaseFirst)
		if err := <-harness.schedule.workDone(); err != nil {
			t.Fatal(err)
		}
		harness.schedule.finishWork()
		if err := <-harness.schedule.workDone(); err != nil {
			t.Fatal(err)
		}
		harness.schedule.finishWork()
		<-nextStarted
		// Let the prior per-unit workers return after publishing completion, so
		// only the pending queue could still own the completed capture.
		synctest.Wait()
		defer func() {
			close(releaseNext)
			<-harness.schedule.workDone()
			harness.schedule.finishWork()
		}()

		runtime.GC()
		if payload.Value() != nil {
			t.Fatal("completed loop work retains its captured payload during the next unit")
		}
	})
}

func queueCapturedLoopWork(schedule *loopSchedule) weak.Pointer[[1024]byte] {
	payload := new([1024]byte) // Larger than a tiny allocation that GC may batch.
	pointer := weak.Make(payload)
	schedule.runWork(func(context.Context) error {
		runtime.KeepAlive(payload)
		return nil
	})
	return pointer
}
