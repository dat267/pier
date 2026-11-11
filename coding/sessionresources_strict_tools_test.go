package coding

import (
	"testing"

	"github.com/dat267/pier/ai"
)

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
