package coding

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// cancelOnSecondErrContext deterministically models cancellation after the
// tool's entry check, during its filesystem work.
type cancelOnSecondErrContext struct {
	context.Context
	checks int
	done   chan struct{}
}

func newCancelOnSecondErrContext() *cancelOnSecondErrContext {
	return &cancelOnSecondErrContext{Context: context.Background(), done: make(chan struct{})}
}

func (c *cancelOnSecondErrContext) Done() <-chan struct{} { return c.done }

func (c *cancelOnSecondErrContext) Err() error {
	c.checks++
	if c.checks >= 2 {
		select {
		case <-c.done:
		default:
			close(c.done)
		}
		return context.Canceled
	}
	return nil
}

// pi v1.4.0 core/tools/ls.ts rejects with "Operation aborted" if its
// AbortSignal fires during the awaited filesystem operations.
func TestLsToolAbortsWhenCanceledDuringFilesystemWork(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "entry.txt"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := newCancelOnSecondErrContext()
	_, err := CreateLsTool(dir).Execute("call", json.RawMessage(`{}`), ctx, nil)
	if err == nil || err.Error() != "Operation aborted" {
		t.Fatalf("ls error = %v; want Operation aborted", err)
	}
}
