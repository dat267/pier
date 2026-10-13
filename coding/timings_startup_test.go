package coding

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestTimingsStartupFlag follows packages/coding-agent/src/core/timings.ts:
// PI_TIMING is captured at initialization, not reread when recording a mark.
func TestTimingsStartupFlag(t *testing.T) {
	if os.Getenv("PIER_TIMING_TEST_CHILD") == "1" {
		enabledAtStart := os.Getenv("PI_TIMING") == "1"
		if enabledAtStart {
			t.Setenv("PI_TIMING", "")
		} else {
			t.Setenv("PI_TIMING", "1")
		}
		SetTimingsEnabled(nil)
		var output bytes.Buffer
		SetTimingsOutput(&output)
		ResetTimings(TimingMain)
		Time("after-env-change", TimingMain)
		PrintTimings()
		if got := strings.Contains(output.String(), "after-env-change"); got != enabledAtStart {
			t.Fatalf("timing output after env change = %q, enabled at startup = %v", output.String(), enabledAtStart)
		}
		return
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "1"} {
		t.Run("PI_TIMING="+value, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, "-test.run=^TestTimingsStartupFlag$")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "PI_TIMING=") && !strings.HasPrefix(entry, "PIER_TIMING_TEST_CHILD=") {
					command.Env = append(command.Env, entry)
				}
			}
			command.Env = append(command.Env, "PI_TIMING="+value, "PIER_TIMING_TEST_CHILD=1")
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("startup flag subprocess: %v\n%s", err, output)
			}
		})
	}
}
