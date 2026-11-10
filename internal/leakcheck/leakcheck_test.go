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
