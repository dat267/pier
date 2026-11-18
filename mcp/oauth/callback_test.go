package oauth

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
	"time"
)

// callback.ts runs timeout and request handlers on one JS event loop. Go timer
// callbacks can run later, after the state has been replaced by a new waiter.
func TestStaleCallbackTimeoutDoesNotDeleteReplacementWaiter(t *testing.T) {
	const state = "same-state"
	oldEntry := &pendingCallback{ch: make(chan callbackResult, 1)}
	newEntry := &pendingCallback{ch: make(chan callbackResult, 1)}
	server := &OAuthCallbackServer{pending: map[string]*pendingCallback{state: newEntry}}

	server.expirePending(state, oldEntry)
	if server.pending[state] != newEntry {
		t.Fatal("stale timeout removed the replacement waiter")
	}
	select {
	case result := <-oldEntry.ch:
		t.Fatalf("stale timeout delivered %v to old waiter", result.err)
	default:
	}
}

// The redirect listener is loopback-only, but any local process can connect and
// send nothing. Without ReadHeaderTimeout that connection holds a handler
// goroutine and a descriptor for as long as it likes (gosec G112).
func TestCallbackServerDropsAStalledConnection(t *testing.T) {
	previous := callbackReadHeaderTimeout
	callbackReadHeaderTimeout = 50 * time.Millisecond
	t.Cleanup(func() { callbackReadHeaderTimeout = previous })

	server, err := ListenCallbackServer(context.Background(), OAuthCallbackServerOptions{Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close(context.Background()) }()
	parsed, err := url.Parse(server.RedirectURL)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("the server answered a connection that sent no request")
	} else {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			t.Fatalf("the server kept a stalled connection open for %v", 2*time.Second)
		}
	}
}

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
