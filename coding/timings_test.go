package coding

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
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

// printTimings reads the namespace Map and writes inline, which is safe in
// JavaScript because it is single-threaded. Go has no such guarantee, and
// PrintTimings is exported: two goroutines printing at once wrote one io.Writer
// concurrently (caught by -race). The write now happens under the mutex.
func TestPrintTimingsIsSafeForConcurrentCallers(t *testing.T) {
	SetTimingsEnabled(new(true))
	defer SetTimingsEnabled(nil)
	var buffer bytes.Buffer
	SetTimingsOutput(&buffer)
	defer SetTimingsOutput(nil)

	ResetTimings(TimingMain)
	Time("one", TimingMain)

	var waitGroup sync.WaitGroup
	for range 8 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for range 20 {
				PrintTimings()
			}
		}()
	}
	waitGroup.Wait()
	if buffer.Len() == 0 {
		t.Fatal("nothing was printed")
	}
}
