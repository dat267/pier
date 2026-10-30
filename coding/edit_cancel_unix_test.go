//go:build unix

package coding

import (
	"context"
	"encoding/json"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// pi v1.4.0 core/tools/edit.ts checks AbortSignal immediately after readFile,
// before attempting text replacement.
func TestEditToolReturnsAbortBeforeReplacementWhenCanceledDuringRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stream.txt")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tool := CreateEditTool(filepath.Dir(path))
	finished := make(chan error, 1)
	go func() {
		_, err := tool.Execute("call", json.RawMessage(`{"path":"stream.txt","edits":[{"oldText":"missing","newText":"replacement"}]}`), ctx, nil)
		finished <- err
	}()

	writer := openFIFOForReadTest(t, path, 2*time.Second)
	cancel()
	if _, err := writer.Write([]byte("original content")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-finished:
		if err == nil || err.Error() != "Operation aborted" {
			t.Fatalf("edit result error = %v; want Operation aborted", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("edit tool did not finish after FIFO read was released")
	}
}
