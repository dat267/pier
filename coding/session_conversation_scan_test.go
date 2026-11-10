package coding

import (
	"testing"
	"time"
)

// persistEntry decides whether to create the session file by calling
// hasConversation, which re-decodes every entry's JSON on every append until a
// user or assistant message exists. That is O(n^2) in the number of entries
// before the first such message, and it runs under SessionManager.mu, so it
// also lengthens the run-lifecycle lock hold that an idle bash append takes
// (A483).
//
// Reference: pi v1.5.0 f9a26365 core/session-manager.ts:1167 _hasConversation
// reads `e.message.role` from the already-parsed entry, so it never decodes;
// the scan is linear in the array but has no per-entry parse cost.
//
// Invariant: the work of one append must not grow with the number of entries
// already appended.
func TestAppendCostDoesNotGrowBeforeFirstConversation(t *testing.T) {
	manager, _ := newTestSession(t)
	// A bash execution message is a "message" entry whose role is neither
	// user nor assistant, so it never ends the scan.
	msg := CreateBashExecutionMessage("echo", "x", nil, false, false, "", false, 0, nil)

	timeAppends := func(count int) time.Duration {
		start := time.Now()
		for i := 0; i < count; i++ {
			manager.AppendMessage(msg)
		}
		return time.Since(start)
	}
	// Warm: JITs, first allocation, and the initial file decision.
	timeAppends(50)
	early := timeAppends(200)
	timeAppends(600)
	late := timeAppends(200)

	// The same 200 appends must cost about the same early and late. A factor of
	// four is far looser than the measured quadratic growth and far tighter
	// than the noise on a warm filesystem.
	if late > 4*early {
		t.Fatalf("append cost grew with entry count: first 200 = %v, last 200 = %v (%.1fx)",
			early, late, float64(late)/float64(early))
	}
	t.Logf("first 200 = %v, last 200 = %v", early, late)
}
