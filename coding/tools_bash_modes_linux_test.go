//go:build linux

package coding

import (
	"os"
	"testing"
)

func TestBashOutputTempFileIsPrivate(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	accumulator := NewOutputAccumulator(100, 1, "private-output-test")
	if err := accumulator.Append([]byte("full output")); err != nil {
		t.Fatal(err)
	}
	path := accumulator.Snapshot(false).FullOutputPath
	if path == "" {
		t.Fatal("truncated output has no persisted file")
	}
	if err := accumulator.CloseTempFile(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("output file mode = %#o; want private mode 0600", got)
	}
}
