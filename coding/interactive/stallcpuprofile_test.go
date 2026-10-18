package interactive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A phase still running at stallCPUProfileAfter is CPU-profiled, and the final
// stall record names the profile file: the goroutine dump cannot attribute a
// completed render (the dump fires in the deferred phase end, when the loop is
// already back in the profiler), but the Oct 17 render burst (6 paints at
// 333-449ms) needs to name the component that dominated.
func TestStallPhaseCapturesCPUProfile(t *testing.T) {
	oldAfter := stallCPUProfileAfter
	stallCPUProfileAfter = 2 * time.Millisecond
	defer func() { stallCPUProfileAfter = oldAfter }()

	dir := t.TempDir()
	w := &RunWiring{StallLogPath: filepath.Join(dir, "pier-stall.log"), StallLogThreshold: time.Millisecond}
	done := w.phase("render")
	start := time.Now()
	sink := 0
	for time.Since(start) < 30*time.Millisecond {
		for i := 0; i < 10000; i++ {
			sink += i
		}
	}
	_ = sink
	done()

	record, err := os.ReadFile(w.StallLogPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(record), "cpu-profile=") {
		t.Fatalf("stall record does not name a cpu profile: %s", record)
	}
	entries, err := filepath.Glob(filepath.Join(dir, "pier-stall-cpu-*.pprof"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("cpu profile files = %v, want one", entries)
	}
	data, err := os.ReadFile(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	// CPU profiles are gzipped protobuf.
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		t.Fatalf("profile file is not gzipped pprof data (%d bytes)", len(data))
	}
}

// A phase that finishes before the CPU watchdog fires writes no profile file,
// but still gets its stall record.
func TestStallPhaseSkipsCPUProfileWhenFast(t *testing.T) {
	oldAfter := stallCPUProfileAfter
	stallCPUProfileAfter = time.Second
	defer func() { stallCPUProfileAfter = oldAfter }()

	dir := t.TempDir()
	w := &RunWiring{StallLogPath: filepath.Join(dir, "pier-stall.log"), StallLogThreshold: time.Millisecond}
	done := w.phase("render")
	time.Sleep(3 * time.Millisecond)
	done()

	entries, err := filepath.Glob(filepath.Join(dir, "pier-stall-cpu-*.pprof"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("cpu profile files = %v, want none", entries)
	}
	record, err := os.ReadFile(w.StallLogPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(record), `slow UI phase "render"`) {
		t.Fatalf("stall record missing: %s", record)
	}
	if strings.Contains(string(record), "cpu-profile=") {
		t.Fatalf("fast phase record names a cpu profile: %s", record)
	}
}

// Profiling only arms when stall recording is on at all.
func TestStallPhaseCPUProfileDisabledWithoutStallLog(t *testing.T) {
	oldAfter := stallCPUProfileAfter
	stallCPUProfileAfter = time.Nanosecond
	defer func() { stallCPUProfileAfter = oldAfter }()

	w := &RunWiring{StallLogPath: "", StallLogThreshold: time.Millisecond}
	done := w.phase("render")
	time.Sleep(5 * time.Millisecond)
	done() // must not panic or start a profile
}

// stallCPUThresholdFromEnv mirrors the record threshold's env override: the
// 250ms default targets rare heavy stalls, and a session hunting lighter ones
// sets PIER_STALL_CPU_MS (0 disables profiling) so every recorded stall also
// carries a profile.
func TestStallCPUThresholdFromEnv(t *testing.T) {
	cases := []struct {
		env  string
		set  bool
		want time.Duration
	}{
		{env: "", set: false, want: 250 * time.Millisecond},
		{env: "0", set: true, want: 0},
		{env: "100", set: true, want: 100 * time.Millisecond},
		{env: "garbage", set: true, want: 250 * time.Millisecond},
		{env: "-3", set: true, want: 250 * time.Millisecond},
	}
	for _, tc := range cases {
		if tc.set {
			t.Setenv("PIER_STALL_CPU_MS", tc.env)
		} else {
			t.Setenv("PIER_STALL_CPU_MS", "")
			os.Unsetenv("PIER_STALL_CPU_MS")
		}
		if got := stallCPUThresholdFromEnv(); got != tc.want {
			t.Errorf("PIER_STALL_CPU_MS=%q (set=%v): got %v, want %v", tc.env, tc.set, got, tc.want)
		}
	}
}
