package tui

import "testing"

// pi v1.4.0 (4a080252), packages/tui/src/utils.ts AnsiCodeTracker.process:
// the SGR parameters come from the leftmost /\x1b\[([\d;]*)m/ match inside the
// consumed code. A truncated CSI followed by a real reset is therefore one
// greedily consumed code whose embedded reset still applies.
func TestANSITrackerFindsResetInsideMalformedCode(t *testing.T) {
	var tracker ansiCodeTracker
	tracker.clear()
	tracker.process("\x1b[31m")
	if got := tracker.activeCodes(); got == "" {
		t.Fatal("red was not tracked")
	}
	tracker.process("\x1b[\x1b[0m")
	if got := tracker.activeCodes(); got != "" {
		t.Fatalf("embedded reset was ignored: active codes %q", got)
	}
}

func TestExtractSegmentsAppliesEmbeddedReset(t *testing.T) {
	segments := ExtractSegments("\x1b[31m\x1b[\x1b[0mabc", 0, 0, 1, false)
	if segments.After != "a" {
		t.Fatalf("after = %q; want %q (the embedded reset must clear the inherited red)", segments.After, "a")
	}
}
