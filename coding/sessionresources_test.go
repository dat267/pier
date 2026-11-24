package coding

import (
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Round 118 tests: session resource cleanups.

func TestSessionResourceCleanups(t *testing.T) {
	var cleaned []string
	unregisterOne := RegisterSessionResourceCleanup(func(sessionID string) {
		cleaned = append(cleaned, "one:"+sessionID)
	})
	defer unregisterOne()
	unregisterTwo := RegisterSessionResourceCleanup(func(sessionID string) {
		cleaned = append(cleaned, "two:"+sessionID)
	})
	defer unregisterTwo()

	if err := CleanupSessionResources("session-1"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(cleaned, ",") != "one:session-1,two:session-1" {
		t.Fatalf("cleaned = %v", cleaned)
	}

	// Unregistration removes only that cleanup.
	unregisterTwo()
	cleaned = nil
	if err := CleanupSessionResources("session-2"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(cleaned, ",") != "one:session-2" {
		t.Fatalf("cleaned = %v", cleaned)
	}

	// A panicking cleanup is reported without stopping the others.
	unregisterBoom := RegisterSessionResourceCleanup(func(sessionID string) { panic("boom") })
	defer unregisterBoom()
	cleaned = nil
	err := CleanupSessionResources("session-3")
	if err == nil || !strings.Contains(err.Error(), "Failed to cleanup session resources") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
	if strings.Join(cleaned, ",") != "one:session-3" {
		t.Fatalf("cleaned = %v", cleaned)
	}
}

// pi v1.5.2 (b4afdddf) api/strict-tool-limits.ts registers the latch's cleanup
// with cleanupSessionResources, so ending a session forgets it. The latch lives
// in ai here, and this is where the session's resources are released.
func TestCleanupSessionResourcesForgetsStrictToolLimitLatch(t *testing.T) {
	t.Cleanup(func() { ai.ResetStrictToolLimits("") })
	ai.MarkStrictToolLimitsExceeded("strict-session")
	ai.MarkStrictToolLimitsExceeded("other-session")

	if err := CleanupSessionResources("strict-session"); err != nil {
		t.Fatal(err)
	}
	if ai.HasExceededStrictToolLimits("strict-session") {
		t.Fatal("CleanupSessionResources kept the session's latch")
	}
	if !ai.HasExceededStrictToolLimits("other-session") {
		t.Fatal("CleanupSessionResources cleared another session's latch")
	}

	// An empty session id releases every session, like upstream's else branch.
	if err := CleanupSessionResources(""); err != nil {
		t.Fatal(err)
	}
	if ai.HasExceededStrictToolLimits("other-session") {
		t.Fatal("an empty cleanup kept a latch")
	}
}
