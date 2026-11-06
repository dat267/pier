package coding

import (
	"runtime"
	"sync"
	"testing"
)

// A kill that starts before the wait finishes runs; once the wait has
// finished, a late timer or abort must not signal the reaped group (whose pid
// can be reused) or rewrite the result as a timeout.
func TestKillGateRunsOnlyBeforeFinish(t *testing.T) {
	var gate killGate
	killed := 0
	if !gate.runKill(func() { killed++ }) {
		t.Fatal("kill before finish did not run")
	}
	gate.finish()
	if gate.runKill(func() { killed++ }) {
		t.Fatal("kill after finish ran")
	}
	if killed != 1 {
		t.Fatalf("kills = %d, want 1", killed)
	}
}

// Interleaving: a kill holds the gate while it runs, a concurrent finish waits
// for it, and a later kill is skipped. This is the timeout-vs-exit window.
func TestKillGateSerializesWithFinish(t *testing.T) {
	var gate killGate
	entered := make(chan struct{})
	release := make(chan struct{})
	killed := 0
	var killWG sync.WaitGroup
	killWG.Add(1)
	go func() {
		defer killWG.Done()
		gate.runKill(func() {
			killed++
			close(entered)
			<-release
		})
	}()
	<-entered
	finished := make(chan struct{})
	go func() {
		gate.finish()
		close(finished)
	}()
	// Let the finish goroutine run; with the gate it cannot complete while the
	// kill is in flight, so only a broken gate closes finished here.
	for range 1000 {
		runtime.Gosched()
	}
	select {
	case <-finished:
		t.Fatal("finish returned while a kill was still in flight")
	default:
	}
	close(release)
	killWG.Wait()
	<-finished
	if gate.runKill(func() { killed++ }) {
		t.Fatal("kill after finish ran")
	}
	if killed != 1 {
		t.Fatalf("kills = %d, want 1", killed)
	}
}
