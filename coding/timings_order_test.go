package coding

import (
	"bytes"
	"strings"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) core/timings.ts printTimings iterates the
// namespace Map, which yields insertion order: main is reset first at startup,
// so its group prints before the extensions group. Pier sorted the names
// alphabetically, which put "extensions" first, contradicting both the
// reference and its own "main before extensions" comment.
func TestPrintTimingsKeepsNamespaceInsertionOrder(t *testing.T) {
	enabled := true
	SetTimingsEnabled(&enabled)
	t.Cleanup(func() {
		SetTimingsEnabled(nil)
		// The namespace registry is package state; leave it as found.
		timingsMu.Lock()
		timingNamespaces = map[TimingNamespace]*timingState{}
		timingOrder = nil
		timingsMu.Unlock()
	})
	var output bytes.Buffer
	SetTimingsOutput(&output)
	t.Cleanup(func() { SetTimingsOutput(nil) })

	ResetTimings(TimingMain)
	Time("boot", TimingMain)
	ResetTimings(TimingExtensions)
	Time("load", TimingExtensions)

	PrintTimings()

	printed := output.String()
	mainAt := strings.Index(printed, "Startup Timings: main")
	extensionsAt := strings.Index(printed, "Startup Timings: extensions")
	if mainAt < 0 || extensionsAt < 0 {
		t.Fatalf("timings output = %q, want both namespace groups", printed)
	}
	if mainAt > extensionsAt {
		t.Fatalf("timings output printed extensions before main:\n%s", printed)
	}
}
