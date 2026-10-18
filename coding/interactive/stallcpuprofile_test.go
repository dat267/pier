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
	dir := t.TempDir()
	w := &RunWiring{StallLogPath: filepath.Join(dir, "pier-stall.log"), StallLogThreshold: time.Millisecond, StallLogCPUAfter: 2 * time.Millisecond}
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
	dir := t.TempDir()
	w := &RunWiring{StallLogPath: filepath.Join(dir, "pier-stall.log"), StallLogThreshold: time.Millisecond, StallLogCPUAfter: time.Second}
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
	w := &RunWiring{StallLogPath: "", StallLogThreshold: time.Millisecond, StallLogCPUAfter: time.Nanosecond}
	done := w.phase("render")
	time.Sleep(5 * time.Millisecond)
	done() // must not panic or start a profile
}

// The CPU-profile threshold defaults to the record threshold, so every stall
// that earns a record also carries its profile: the Oct 18 live records
// (110-125ms streaks) sat between the old 250ms profile default and the 100ms
// record default and were never attributed. An explicit PIER_STALL_CPU_MS
// overrides (0 disables).
func TestStallCPUThresholdFromEnv(t *testing.T) {
	cases := []struct {
		env  string
		set  bool
		want time.Duration
	}{
		{env: "", set: false, want: 100 * time.Millisecond},
		{env: "0", set: true, want: 0},
		{env: "250", set: true, want: 250 * time.Millisecond},
		{env: "garbage", set: true, want: 100 * time.Millisecond},
		{env: "-3", set: true, want: 100 * time.Millisecond},
	}
	for _, tc := range cases {
		if tc.set {
			t.Setenv("PIER_STALL_CPU_MS", tc.env)
		} else {
			t.Setenv("PIER_STALL_CPU_MS", "")
			os.Unsetenv("PIER_STALL_CPU_MS")
		}
		if got := stallCPUThresholdFromEnv(100 * time.Millisecond); got != tc.want {
			t.Errorf("PIER_STALL_CPU_MS=%q (set=%v): got %v, want %v", tc.env, tc.set, got, tc.want)
		}
	}
}

// A keystroke's round trip crosses every phase the loop runs while it waits;
// each phase can be under the stall threshold while the user-feel latency is
// not. The live case: two ~84ms reads at startup with PIER_STALL_MS=50 wrote no
// record because the loop's phases were each under 50ms. A contiguous run of
// short phases is recorded as one busy streak naming every phase it crossed
// (the profile path has its own tests; the CPU threshold here is effectively
// off).
func TestStallLogRecordsBusyStreakAcrossPhases(t *testing.T) {
	dir := t.TempDir()
	w := &RunWiring{StallLogPath: filepath.Join(dir, "pier-stall.log"), StallLogThreshold: 50 * time.Millisecond, StallLogCPUAfter: time.Hour}
	run := func(name string) {
		done := w.phase(name)
		time.Sleep(20 * time.Millisecond)
		done()
		time.Sleep(2 * time.Millisecond)
	}
	run("events") // streak 20ms
	run("events") // streak 42ms
	run("render") // crossing happens inside this phase: streak 64ms

	record, err := os.ReadFile(w.StallLogPath)
	if err != nil {
		t.Fatalf("no stall record for the busy streak: %v", err)
	}
	text := string(record)
	if !strings.Contains(text, `phases=events+events+render`) {
		t.Fatalf("streak record does not name the phases: %s", text)
	}
	if strings.Contains(text, `slow UI phase "render"`) || strings.Contains(text, `slow UI phase "events"`) {
		t.Fatalf("individual phases must not be recorded when only the streak is slow: %s", text)
	}
	if entries, _ := filepath.Glob(filepath.Join(dir, "pier-stall-cpu-*.pprof")); len(entries) != 0 {
		t.Fatalf("cpu profiles = %v, want none", entries)
	}
}

// A long idle gap between phases starts a new streak: spaced-out short phases
// are not a busy loop, and each is under the threshold on its own.
func TestStallLogIdleGapBreaksStreak(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pier-stall.log")
	w := &RunWiring{StallLogPath: path, StallLogThreshold: 50 * time.Millisecond}
	done := w.phase("events")
	time.Sleep(10 * time.Millisecond)
	done()
	time.Sleep(100 * time.Millisecond) // the loop went idle in between
	done = w.phase("render")
	time.Sleep(30 * time.Millisecond)
	done()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		info, _ := os.Stat(path)
		data, _ := os.ReadFile(path)
		t.Fatalf("spaced-out short phases recorded: %v (%d bytes: %s)", err, info.Size(), data)
	}
}

// A streak is recorded once: the record fires when the contiguous run first
// crosses the threshold, and later phase-ends in the same run stay silent (a
// streaming turn keeps the loop busy for seconds; one record per streak, not
// one per phase — the first live streak produced 30+ records in 4 seconds).
func TestStallLogStreakRecordsOnce(t *testing.T) {
	dir := t.TempDir()
	w := &RunWiring{StallLogPath: filepath.Join(dir, "pier-stall.log"), StallLogThreshold: 50 * time.Millisecond}
	run := func(name string) {
		done := w.phase(name)
		time.Sleep(20 * time.Millisecond)
		done()
		time.Sleep(2 * time.Millisecond)
	}
	run("events") // streak 20ms
	run("events") // streak 42ms
	done := w.phase("render")
	time.Sleep(30 * time.Millisecond) // streak ~74ms: crosses, profile arms at ~60ms with headroom
	done()
	run("render") // the flood bug would record again
	run("render")

	record, err := os.ReadFile(w.StallLogPath)
	if err != nil {
		t.Fatalf("no streak record: %v", err)
	}
	streaks := strings.Count(string(record), `slow UI phase "streak"`)
	if streaks != 1 {
		t.Fatalf("streak recorded %d times, want 1:\n%s", streaks, record)
	}
}

// A streak's CPU profile starts at the threshold crossing, so it only covers
// the streak's tail (the first live capture: 14ms window, one sample, and that
// sample was a background goroutine). The dump that matters is the one taken
// at the crossing itself, while the loop is mid-phase: a "streak-start" record
// carries it, once per streak.
func TestStreakStartRecordDumpsTheLoopMidPhase(t *testing.T) {
	dir := t.TempDir()
	w := &RunWiring{StallLogPath: filepath.Join(dir, "pier-stall.log"), StallLogThreshold: 50 * time.Millisecond, StallLogCPUAfter: 50 * time.Millisecond}
	run := func(name string) {
		done := w.phase(name)
		time.Sleep(20 * time.Millisecond)
		done()
		time.Sleep(2 * time.Millisecond)
	}
	run("events") // streak 20ms
	run("events") // streak 42ms
	run("render") // crosses here: the mid-streak dump fires mid-phase

	record, err := os.ReadFile(w.StallLogPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(record)
	if !strings.Contains(text, `slow UI phase "streak-start"`) {
		t.Fatalf("no mid-streak dump record:\n%s", text)
	}
	if !strings.Contains(text, "goroutine profile:") {
		t.Fatalf("mid-streak record carries no dump:\n%s", text)
	}
	if strings.Count(text, `streak-start`) != 1 {
		t.Fatalf("mid-streak record written more than once:\n%s", text)
	}
}

// A busy run of short phases (streaming: beat+events repeated) is attributed by
// the loop time it actually executed, not its wall-clock span. The record is
// written at the phase end where the busy sum crosses the threshold, with a
// dump of the just-finished phase. A fake clock gives exact durations (a race
// build inflates real sleeps).
func TestStreakRecordsBusySumAcrossShortPhases(t *testing.T) {
	oldGap := stallStreakGap
	stallStreakGap = 5 * time.Millisecond
	defer func() { stallStreakGap = oldGap }()
	oldNow := stallClockNow
	defer func() { stallClockNow = oldNow }()

	dir := t.TempDir()
	w := &RunWiring{StallLogPath: filepath.Join(dir, "pier-stall.log"), StallLogThreshold: 50 * time.Millisecond, StallLogCPUAfter: time.Hour}
	now := time.Unix(1000, 0)
	stallClockNow = func() time.Time { return now }
	for i := 0; i < 8; i++ {
		done := w.phase("events")
		now = now.Add(10 * time.Millisecond)
		done()
		now = now.Add(2 * time.Millisecond)
	}
	record, err := os.ReadFile(w.StallLogPath)
	if err != nil {
		t.Fatalf("no streak record for 80ms of busy phases: %v", err)
	}
	text := string(record)
	if !strings.Contains(text, `slow UI phase "streak"`) || !strings.Contains(text, "phases=events") {
		t.Fatalf("streak record does not name the phases:\n%s", text)
	}
	if !strings.Contains(text, "span=") {
		t.Fatalf("streak record does not report the busy sum and span:\n%s", text)
	}
	if !strings.Contains(text, "goroutine profile:") {
		t.Fatalf("streak record carries no dump:\n%s", text)
	}
}

// The false positive behind the 18:46 records: a streaming delta cadence with
// sub-gap select waits spans >100ms while the loop executes far less. The busy
// sum must gate the record, so a span over the threshold with under-threshold
// work is not reported.
func TestStreakBusySumIgnoresIdleSelectWaits(t *testing.T) {
	oldGap := stallStreakGap
	stallStreakGap = 10 * time.Millisecond
	defer func() { stallStreakGap = oldGap }()
	oldNow := stallClockNow
	defer func() { stallClockNow = oldNow }()

	dir := t.TempDir()
	path := filepath.Join(dir, "pier-stall.log")
	w := &RunWiring{StallLogPath: path, StallLogThreshold: 100 * time.Millisecond, StallLogCPUAfter: time.Hour}
	now := time.Unix(2000, 0)
	stallClockNow = func() time.Time { return now }
	for i := 0; i < 10; i++ { // busy 30ms over a 120ms span of one streak
		done := w.phase("events")
		now = now.Add(3 * time.Millisecond)
		done()
		now = now.Add(9 * time.Millisecond)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		data, _ := os.ReadFile(path)
		t.Fatalf("a >100ms span with 30ms of work recorded a stall:\n%s", data)
	}
}
