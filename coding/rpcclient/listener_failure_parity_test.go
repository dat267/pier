package rpcclient

import (
	"sync/atomic"
	"testing"
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
