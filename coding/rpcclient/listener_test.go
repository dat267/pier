package rpcclient

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// pi v1.4.0 modes/rpc/rpc-client.ts handleLine catches exceptions from event
// listeners along with JSON errors. A failed callback stops that event's
// dispatch but must not kill the stdout reader or pending command responses.
func TestClientListenerFailureDoesNotKillReader(t *testing.T) {
	client := startFakeClient(t, nil)
	var first, second atomic.Int32
	client.OnEvent(func(Event) {
		if first.Add(1) == 1 {
			panic("fixture listener failed")
		}
	})
	client.OnEvent(func(Event) { second.Add(1) })
	for range 2 {
		state, err := client.State()
		if err != nil || state.SessionID != "fake" {
			t.Fatalf("command response after listener failure: state=%+v error=%v", state, err)
		}
	}
	if got := first.Load(); got != 2 {
		t.Fatalf("first listener calls = %d, want 2", got)
	}
	if got := second.Load(); got != 1 {
		t.Fatalf("second listener calls = %d, want only the event after the failure", got)
	}
}

// pi v1.4.0 modes/rpc/rpc-client.ts iterates a snapshot of a Set, so listeners
// run in registration order. Go map iteration would randomize that order, which
// is observable for any listener with side effects.
func TestClientListenersRunInRegistrationOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess round trip")
	}
	client := startFakeClient(t, nil)
	var order []string
	client.OnEvent(func(Event) { order = append(order, "first") })
	client.OnEvent(func(Event) { order = append(order, "second") })
	for range 25 {
		order = order[:0]
		if _, err := client.State(); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(order, ","); got != "first,second" {
			t.Fatalf("listener order = %q, want first,second", got)
		}
	}
}

// pi v1.4.0 modes/rpc/rpc-client.ts start claims this.process before its first
// await, so overlapping starts cannot spawn more than one peer. Go must make
// that admission atomic before releasing its mutex to spawn the subprocess.
func TestClientConcurrentStartAdmitsOnePeer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := New(Options{
		CLIPath: os.Args[0], PeerArgs: []string{"-test.run=^TestRPCClientFakeServer$"},
		Env: map[string]string{"PIER_RPC_CLIENT_FAKE": "1"},
	})
	t.Cleanup(func() { cancel(); client.Stop() })
	const callers = 8
	gate := make(chan struct{})
	results := make(chan error, callers)
	var ready sync.WaitGroup
	ready.Add(callers)
	for range callers {
		go func() {
			ready.Done()
			<-gate
			results <- client.Start(ctx)
		}()
	}
	ready.Wait()
	close(gate)
	admitted := 0
	for range callers {
		err := <-results
		if err == nil {
			admitted++
		} else if err.Error() != "Client already started" {
			t.Errorf("unexpected startup error: %v", err)
		}
	}
	if admitted != 1 {
		t.Fatalf("admitted %d overlapping starts, want exactly 1", admitted)
	}
	if _, err := client.State(); err != nil {
		t.Fatalf("admitted peer is not usable: %v", err)
	}
}

// pi v1.4.0 modes/rpc/rpc-client.ts stop can be initiated by an event listener:
// its promise waits for child exit, not for its own stdout dispatch to return.
func TestClientEventListenerCanStopClient(t *testing.T) {
	client := startFakeClient(t, nil)
	stopped := make(chan struct{})
	client.OnEvent(func(Event) {
		client.Stop()
		close(stopped)
	})
	requestDone := make(chan struct{})
	go func() {
		_, _ = client.State() // The peer emits an event before replying.
		close(requestDone)
	}()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("event listener Stop waits for its own stdout reader")
	}
	select {
	case <-requestDone:
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight request did not settle after listener Stop")
	}
}
