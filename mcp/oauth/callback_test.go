package oauth

import "testing"

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
