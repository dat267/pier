package coding

import (
	"os"
	"testing"
)

func TestTempOutputSweepKeepsAnotherAccumulatorFile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

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
