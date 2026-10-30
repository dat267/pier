package oauth

import (
	"context"
	"testing"
	"time"
)

// pi v1.4.0 callback.ts uses ?? for timeoutMs, so explicit zero reaches
// setTimeout and expires after its 1ms minimum rather than selecting 5 minutes.
func TestCallbackExplicitZeroTimeoutIsImmediate(t *testing.T) {
	server, err := ListenCallbackServer(context.Background(), OAuthCallbackServerOptions{TimeoutMs: 0, TimeoutMsSet: true})
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	closeServer := func() {
		if !closed {
			closed = true
			_ = server.Close(context.Background())
		}
	}
	t.Cleanup(closeServer)

	result := make(chan error, 1)
	go func() {
		_, err := server.WaitForCallback("zero-timeout")
		result <- err
	}()
	select {
	case err := <-result:
		if err == nil || err.Error() != "OAuth callback timed out" {
			t.Fatalf("callback error = %v, want timeout", err)
		}
	case <-time.After(500 * time.Millisecond):
		closeServer()
		select {
		case <-result:
		case <-time.After(time.Second):
			t.Fatal("callback waiter remained blocked after server close")
		}
		t.Fatal("explicit zero selected the callback default timeout")
	}
}
