package coding

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// D161: the port sweeps its own temp output files instead of leaving them to the
// OS. These pin the three properties that matter: this port's files are the only
// ones touched, the oldest go first, and the file being written now survives.
func TestSweepTempOutputFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	make := func(name string, size int, age time.Duration) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), 0o600); err != nil {
			t.Fatal(err)
		}
		stamp := now.Add(-age)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		return path
	}
	exists := func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	}

	// Names run the opposite way to age, so a sweep that trusts directory order
	// (ReadDir is alphabetical) deletes the wrong file and fails below.
	oldest := make("pi-bash-zzz-oldest.log", 100, 3*time.Hour)
	older := make("pi-bash-mmm-middle.log", 100, 2*time.Hour)
	newer := make("pi-bash-aaa-newest.log", 100, time.Hour)
	fresh := make("pi-bash-bbb-fresh.log", 0, 0)
	// Not this port's scratch: a different tool's log, a file without the
	// suffix, and a directory that happens to match the name pattern.
	stranger := make("other-tool.log", 100, 4*time.Hour)
	noSuffix := make("pi-bash-not-a-log", 100, 4*time.Hour)
	matchingDir := filepath.Join(dir, "pi-bash-iam-a-dir.log")
	if err := os.Mkdir(matchingDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Under budget: nothing is touched.
	sweepTempOutputFiles(dir, fresh, 300)
	for _, path := range []string{oldest, older, newer, fresh} {
		if !exists(path) {
			t.Fatalf("%s was removed while under budget", filepath.Base(path))
		}
	}

	// Budget 200 of 300 bytes: the oldest goes, the rest stay.
	sweepTempOutputFiles(dir, fresh, 200)
	if exists(oldest) {
		t.Error("the oldest file survived")
	}
	for _, path := range []string{older, newer, fresh} {
		if !exists(path) {
			t.Errorf("%s was removed but it was not the oldest", filepath.Base(path))
		}
	}

	// Budget 0: everything of this port's goes, except the file being written.
	sweepTempOutputFiles(dir, fresh, 0)
	for _, path := range []string{older, newer} {
		if exists(path) {
			t.Errorf("%s survived a zero budget", filepath.Base(path))
		}
	}
	if !exists(fresh) {
		t.Error("the file being written was swept")
	}
	// Anything that is not this port's scratch is still there.
	if !exists(stranger) {
		t.Error("another tool's log was swept")
	}
	if !exists(noSuffix) {
		t.Error("a file without the .log suffix was swept")
	}
	if !exists(matchingDir) {
		t.Error("a directory matching the name pattern was swept")
	}
}

// D161 diverges from pi v1.4.0's core/tools/output-accumulator.ts scratch
// retention; the budget sweep must not evict a separate active accumulator.
func TestTempOutputSweepKeepsAnotherAccumulatorFile(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	t.Setenv("TMP", tempDir)
	t.Setenv("TEMP", tempDir)

	active := NewOutputAccumulator(1, 1, "pi-output")
	if err := active.Append([]byte("large output")); err != nil {
		t.Fatal(err)
	}
	activePath := active.Snapshot(true).FullOutputPath
	if activePath == "" {
		t.Fatal("active accumulator did not persist its output")
	}
	defer func() {
		_ = active.CloseTempFile()
		_ = os.Remove(activePath)
	}()

	// Model a large live output file without allocating or writing 256 MiB.
	if err := os.Truncate(activePath, tempOutputBudgetBytes+1); err != nil {
		t.Fatal(err)
	}

	other := NewOutputAccumulator(1, 1, "pi-output")
	if err := other.Append([]byte("another output")); err != nil {
		t.Fatal(err)
	}
	otherPath := other.Snapshot(false).FullOutputPath
	defer func() {
		_ = other.CloseTempFile()
		_ = os.Remove(otherPath)
	}()

	if _, err := os.Stat(activePath); err != nil {
		t.Fatalf("creating another output removed a still-active accumulator's file: %v", err)
	}
}

// A backgrounded call's output file belongs to a RUNNING call: the finish notice
// tells the model to read it, so the sweep must not delete it even when the
// scratch budget is exceeded and it is the oldest file present. Regression: it
// was not registered as active, so a sweep during a long call removed the path
// the notice later named.
func TestSweepTempOutputFilesKeepsALiveBackgroundOutput(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeAged := func(name string, size int, age time.Duration) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), 0o600); err != nil {
			t.Fatal(err)
		}
		stamp := now.Add(-age)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		return path
	}
	live := filepath.Join(dir, "pi-background-live.log")
	writeAged("pi-output-a.log", 400, time.Minute)
	writeAged("pi-output-b.log", 400, time.Second)
	keep := writeAged("pi-output-keep.log", 0, 0)

	// Create-and-register the background file the way the writer does (O_EXCL,
	// then registered active), age it so a sweep would pick it first, then sweep
	// past the budget.
	file, err := createActiveTempOutput(live)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		unregisterActiveTempOutput(live)
		_ = file.Close()
	}()
	if _, err := file.Write(bytes.Repeat([]byte("x"), 400)); err != nil {
		t.Fatal(err)
	}
	stamp := now.Add(-time.Hour)
	if err := os.Chtimes(live, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	sweepTempOutputFiles(dir, keep, 500)
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("the live background output was swept: %v", err)
	}
}

// ClaimBackgroundToolOutput keeps the file out of the sweep while the call runs
// and returns it to the sweepable set once released.
func TestClaimBackgroundToolOutputScopesTheSweep(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "pi-background-call.log")
	if err := os.WriteFile(live, make([]byte, 400), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "pi-output-keep.log")
	if err := os.WriteFile(keep, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(live, old, old); err != nil {
		t.Fatal(err)
	}

	release := ClaimBackgroundToolOutput(live)
	sweepTempOutputFiles(dir, keep, 100)
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("a claimed output was swept: %v", err)
	}

	release()
	if err := os.Chtimes(live, old, old); err != nil {
		t.Fatal(err)
	}
	sweepTempOutputFiles(dir, keep, 100)
	if _, err := os.Stat(live); err == nil {
		t.Fatal("a released output was not swept")
	}
}
