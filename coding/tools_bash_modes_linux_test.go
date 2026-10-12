//go:build linux

package coding

import (
	"context"
	"os"
	"strings"
	"testing"
)

type modeTestBashOperations struct{}

func (modeTestBashOperations) Exec(_ context.Context, _, _ string, options BashExecOptions) (*int, error) {
	options.OnData([]byte(strings.Repeat("x", DefaultMaxBytes+1)))
	code := 0
	return &code, nil
}

func TestBashExecutorTempFileIsPrivate(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	result, err := ExecuteBashWithOperations(context.Background(), "cmd", t.TempDir(), modeTestBashOperations{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.FullOutputPath == "" {
		t.Fatal("truncated output has no persisted file")
	}
	info, err := os.Stat(result.FullOutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("executor output file mode = %#o; want private mode 0600", got)
	}
}

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
