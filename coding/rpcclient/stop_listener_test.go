package rpcclient

import (
	"testing"
	"time"
)

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
