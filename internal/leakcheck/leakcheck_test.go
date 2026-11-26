package leakcheck

import (
	"strings"
	"testing"
	"time"
)

// A goroutine started after the baseline and still blocked is reported with its
// stack; once it ends, the check comes back clean within the grace period.
func TestWaitForLeaksReportsAGoroutineStartedAfterTheBaseline(t *testing.T) {
	before := goroutineIDs(stacks())
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-release
	}()

	leaked := waitForLeaks(before, 0)
	if len(leaked) != 1 || !strings.Contains(leaked[0], "TestWaitForLeaksReportsAGoroutineStartedAfterTheBaseline") {
		t.Fatalf("leaked = %q, want the blocked goroutine", leaked)
	}

	close(release)
	<-done
	if leaked := waitForLeaks(before, time.Second); len(leaked) != 0 {
		t.Fatalf("leaked after release = %q", leaked)
	}
}

func TestIgnoredFramesAreNotLeaks(t *testing.T) {
	stack := "goroutine 7 [select]:\nos/signal.loop()\n\t/usr/lib/go/src/os/signal/signal_unix.go:22"
	if !isIgnored(stack) {
		t.Fatal("the signal loop should be ignored")
	}
	if got := goroutineID(stack); got != "7" {
		t.Fatalf("id = %q", got)
	}
}

// An ignored frame in the "created by" trailer must NOT hide the goroutine: the
// trailer names the CREATOR, so a worker leaked from inside testing.runFuzzTests
// or anything reached through installExitHook.func1 would never be reported.
// Regression: the whole block was scanned, so every ignored frame silenced a
// leak that merely mentioned it.
func TestIgnoredFrameInTheCreatedByTrailerIsStillALeak(t *testing.T) {
	for _, frame := range ignoredFrames {
		if strings.HasPrefix(frame, "created by ") {
			// An entry naming the trailer describes a goroutine the standard
			// library spawns, which only ever appears there.
			continue
		}
		stack := "goroutine 9 [chan receive]:\n" +
			"pier/coding.(*worker).run(0x0)\n\t/code/worker.go:10 +0x1\n" +
			"created by " + frame + " in goroutine 1\n\t/gopath.go:1 +0x2"
		if isIgnored(stack) {
			t.Fatalf("a leak created by %s was ignored", frame)
		}
		// The same frame as the goroutine's OWN function is still ignored.
		own := "goroutine 9 [chan receive]:\n" + frame + "(0x0)\n\t/go.go:1 +0x1"
		if !isIgnored(own) {
			t.Fatalf("%s as its own frame must be ignored", frame)
		}
	}
}
