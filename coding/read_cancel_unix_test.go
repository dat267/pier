//go:build unix

package coding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// pi v1.4.0 core/tools/read.ts rejects with "Operation aborted" when its
// AbortSignal fires during filesystem awaits and ignores late completion.
func TestReadToolReturnsAbortWhenCanceledDuringMimeSniff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stream.txt")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tool := CreateReadTool(filepath.Dir(path), nil)
	finished := make(chan error, 1)
	go func() {
		_, err := tool.Execute("call", json.RawMessage(`{"path":"stream.txt"}`), ctx, nil)
		finished <- err
	}()

	// The MIME sniffer opens the FIFO and waits for input. Cancel while that
	// filesystem read is pending, then release it with non-image bytes.
	firstWriter := openFIFOForReadTest(t, path, 2*time.Second)
	cancel()
	// Unlike upstream's abort listener, Go cannot interrupt the in-flight
	// synchronous filesystem call (D315).
	select {
	case err := <-finished:
		t.Fatalf("read finished while MIME sniff remained blocked: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	_, writeErr := firstWriter.Write(bytes.Repeat([]byte{'x'}, 1<<20))
	if writeErr != nil && !errors.Is(writeErr, syscall.EPIPE) {
		t.Fatal(writeErr)
	}
	if err := firstWriter.Close(); err != nil {
		t.Fatal(err)
	}

	// Without an abort check after MIME detection, the implementation starts a
	// second read of the FIFO. Releasing that read exposes an incorrect success.
	deadline := time.Now().Add(2 * time.Second)
	for {
		select {
		case err := <-finished:
			if err == nil || err.Error() != "Operation aborted" {
				t.Fatalf("read result error = %v; want Operation aborted", err)
			}
			return
		default:
		}
		fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			writer := os.NewFile(uintptr(fd), path)
			if _, writeErr := writer.Write([]byte("second read")); writeErr != nil {
				t.Fatal(writeErr)
			}
			_ = writer.Close()
			break
		}
		if !errors.Is(err, syscall.ENXIO) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("read tool neither aborted nor opened its second FIFO read")
		}
		time.Sleep(time.Millisecond)
	}

	select {
	case err := <-finished:
		if err == nil || err.Error() != "Operation aborted" {
			t.Fatalf("read result error = %v; want Operation aborted", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("read tool did not finish after the FIFO was released")
	}
}

func openFIFOForReadTest(t *testing.T, path string, timeout time.Duration) *os.File {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			return os.NewFile(uintptr(fd), path)
		}
		if !errors.Is(err, syscall.ENXIO) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("read tool did not open the FIFO for MIME detection")
		}
		time.Sleep(time.Millisecond)
	}
}
