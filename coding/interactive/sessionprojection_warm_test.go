package interactive

import (
	"context"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"runtime"
	"testing"
	"testing/synctest"
	"weak"
)

// TestSessionReplacementDoesNotRebuildTheProjectionOnTheLoop pins A494: the
// worker that opens a session also derives its projection, so the posted rebind
// does not decode every entry on the owner loop. At 12,500 turns that decode was
// 151 ms of a 204 ms rebind.
//
// The check is a count, not a clock: ProjectionBuilds only moves when an
// uncached projection is derived, so any build during the rebind is one the loop
// paid.
func TestSessionReplacementDoesNotRebuildTheProjectionOnTheLoop(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	cwd := t.TempDir()

	session := coding.NewSessionManager(cwd, nil)
	for range 2000 {
		session.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "question"}})
		session.AppendMessage(&ai.AssistantMessage{
			API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
			Content:    ai.ContentList{ai.TextContent{Text: "answer"}},
			StopReason: ai.StopStop,
		})
	}
	path := session.GetSessionFile()

	opened, err := app.openSessionWork(path, "")(context.Background())
	if err != nil {
		t.Fatalf("openSessionWork: %v", err)
	}
	before := opened.ProjectionBuilds()
	if _, err := app.applyOpenedSession(opened, nil); err != nil {
		t.Fatalf("applyOpenedSession: %v", err)
	}
	if after := opened.ProjectionBuilds(); after != before {
		t.Fatalf("the rebind rebuilt the projection %d time(s) on the loop; the worker must warm it", after-before)
	}

	// A wiring without a status reporter still rebinds: the reset is a nil
	// check, and inverting it calls Reset on a nil reporter.
	app.programStatus = nil
	if _, err := app.applyOpenedSession(opened, nil); err != nil {
		t.Fatalf("applyOpenedSession without a status reporter: %v", err)
	}
}

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
