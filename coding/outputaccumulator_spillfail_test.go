package coding

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// TestOutputAccumulatorDropsAFailedSpill pins A505: when the temp file that
// holds oversized output cannot be written, the result must not claim a
// full-output file. Publishing the path before the data is durable, and
// dropping the write error at every call site, told the model to read a partial
// file as the complete output.
//
// The failure is injected through the accumulator's write seam so it needs no
// full filesystem (ENOSPC on a tmpfs is the real case, D161).
func TestOutputAccumulatorDropsAFailedSpill(t *testing.T) {
	spillErr := errors.New("no space left on device")
	cases := []struct {
		name string
		// failAt is the 1-based spill write that fails: 1 is the buffered
		// backlog, 2 the next streamed chunk.
		failAt int
	}{
		{name: "buffered backlog", failAt: 1},
		{name: "streamed chunk", failAt: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			t.Setenv("TMPDIR", tempDir)

			accumulator := NewOutputAccumulator(2, 16, "pi-output")
			writes := 0
			accumulator.spillWrite = func(file *os.File, data []byte) error {
				writes++
				if writes == tc.failAt {
					return spillErr
				}
				_, err := file.Write(data)
				return err
			}

			// The first chunk is buffered; the rest cross the limit and force
			// the spill, which is where the injected failure lands.
			var appendErr error
			for _, chunk := range []string{"line one\n", "line two\n", "line three\n", "line four\n"} {
				if err := accumulator.Append([]byte(chunk)); err != nil {
					appendErr = err
				}
			}
			accumulator.Finish()
			snapshot := accumulator.Snapshot(true)
			_ = accumulator.CloseTempFile()

			if appendErr != nil && !errors.Is(appendErr, spillErr) {
				t.Fatalf("Append error = %v, want nil or the spill error", appendErr)
			}
			if snapshot.FullOutputPath != "" {
				t.Errorf("the result advertises %q after a failed spill; the model would read a partial file as the full output",
					snapshot.FullOutputPath)
			}
			if !snapshot.Truncation.Truncated {
				t.Errorf("the output crossed the limits, so it must still report truncation: %+v", snapshot.Truncation)
			}
			entries, err := os.ReadDir(tempDir)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), ".log") {
					t.Errorf("a failed spill left %q behind; it is scratch nobody can read", entry.Name())
				}
			}
		})
	}
}

// TestBashExecutorDropsAFailedSpill covers the same root cause in the executor
// that streams a shell command's output: it already recorded a failed spill
// write in tempErr and then ignored it, so the result still named a partial
// file (A505).
func TestBashExecutorDropsAFailedSpill(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)

	previousWrite := tempOutputWriteString
	tempOutputWriteString = func(file *os.File, text string) (int, error) {
		return 0, errors.New("no space left on device")
	}
	defer func() { tempOutputWriteString = previousWrite }()

	zero := 0
	result, err := ExecuteBashWithOperations(context.Background(), "cmd", "/tmp",
		&scriptedOps{chunks: []string{jsRepeatString("z", DefaultMaxBytes+64)}, exitCode: &zero},
		&BashExecutorOptions{})
	if err != nil {
		t.Fatalf("ExecuteBashWithOperations: %v", err)
	}
	if result.FullOutputPath != "" {
		t.Errorf("the result advertises %q after a failed spill; the model would read a partial file as the full output",
			result.FullOutputPath)
	}
	if !result.Truncated {
		t.Errorf("the output crossed the limit, so it must still report truncation")
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".log") {
			t.Errorf("a failed spill left %q behind; it is scratch nobody can read", entry.Name())
		}
	}
}
