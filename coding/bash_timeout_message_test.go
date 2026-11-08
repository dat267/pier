package coding

import "testing"

// bash.ts reports String(timeout) (pi v1.5.0
// packages/coding-agent/src/core/tools/bash.ts, the "timeout:" error), so
// 1000000 is not Go's %g "1e+06".
func TestBashTimeoutMessageFormatsLikeJavaScript(t *testing.T) {
	for value, want := range map[float64]string{
		1:       "Command timed out after 1 seconds",
		0.5:     "Command timed out after 0.5 seconds",
		1000000: "Command timed out after 1000000 seconds",
		1e21:    "Command timed out after 1e+21 seconds",
	} {
		if got := bashTimeoutMessage(value); got != want {
			t.Errorf("bashTimeoutMessage(%v) = %q; want %q", value, got, want)
		}
	}
}
