package oauth

import (
	"context"
	"testing"
	"time"
)

// pi v1.4.0 callback.ts passes every positive timeout to setTimeout, which
// clamps delays above 2^31-1 to 1ms.
func TestCallbackWaitTimeoutUsesJavaScriptTimerRange(t *testing.T) {
	server, err := ListenCallbackServer(context.Background(), OAuthCallbackServerOptions{
		TimeoutMs: 1 << 31,
	})
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
		_, err := server.WaitForCallback("timer-range")
		result <- err
	}()
	select {
	case err := <-result:
		if err == nil || err.Error() != "OAuth callback timed out" {
			t.Fatalf("callback result error = %v, want timeout", err)
		}
	case <-time.After(500 * time.Millisecond):
		closeServer()
		select {
		case <-result:
		case <-time.After(time.Second):
			t.Fatal("callback waiter remained blocked after server close")
		}
		t.Fatal("oversized timeout did not fire on the JavaScript timer schedule")
	}
}
