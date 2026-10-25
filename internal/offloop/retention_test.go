package offloop_test

import (
	"runtime"
	"testing"
	"weak"

	"github.com/dat267/pier/internal/offloop"
)

// offloop is the Go-only worker bridge for pi's single-threaded
// packages/coding-agent/src/core/session-manager.ts persistence (D143/D199).
// Completed saves must not retain the captured snapshot while the next job runs.
func TestQueueReleasesCompletedTaskCaptures(t *testing.T) {
	queue := offloop.New()
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	queue.Go(func() { close(firstStarted); <-releaseFirst })
	<-firstStarted

	payload := enqueueCapturedPayload(queue)
	nextStarted := make(chan struct{})
	releaseNext := make(chan struct{})
	queue.Go(func() { close(nextStarted); <-releaseNext })
	defer func() { close(releaseNext); queue.Stop() }()
	close(releaseFirst)
	<-nextStarted

	runtime.GC()
	if payload.Value() != nil {
		t.Fatal("completed task still retains its captured payload while the next task runs")
	}
}

func enqueueCapturedPayload(queue *offloop.Queue) weak.Pointer[[1024]byte] {
	payload := new([1024]byte) // Not a tiny allocation that GC can batch with live data.
	pointer := weak.Make(payload)
	queue.Go(func() { runtime.KeepAlive(payload) })
	return pointer
}
